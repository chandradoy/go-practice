package main

import (
	"context"
	"fmt"
	"github.com/gorilla/mux"
	"github.com/hashicorp/serf/serf"
	"github.com/pkg/errors"
	"golang.org/x/sync/errgroup"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"
)

type Value struct {
	number     int
	generation int
	numMutex   sync.RWMutex
}

func initValue(val int) *Value {
	log.Println("Initializing value.......")
	return &Value{
		number: val,
	}
}

func (n *Value) setValue(newVal int) {
	n.numMutex.Lock()
	defer n.numMutex.Unlock()
	n.number = newVal
	n.generation = n.generation + 1
}

func (n *Value) getValue() (int, int) {
	n.numMutex.RLock()
	defer n.numMutex.RUnlock()
	return n.number, n.generation
}

func (n *Value) notifyValue(curVal int, curGeneration int) bool {
	log.Println("Notifying value....")
	if curGeneration > n.generation {
		log.Print("Aquiring Lock.....")
		n.numMutex.Lock()
		defer n.numMutex.Unlock()
		n.generation = curGeneration
		n.number = curVal
		return true
	}
	return false
}
const membersToNotify = 2

func setupCluster(advertiseAddr string, clusterAddr string) (*serf.Serf, error) {
	log.Println("Broadcast Timeout",serf.DefaultConfig().BroadcastTimeout , "............")
	conf := serf.DefaultConfig()
	conf.Init()
	conf.MemberlistConfig.AdvertiseAddr = advertiseAddr

	cluster, err := serf.Create(conf)
	if err != nil {
		return nil, errors.Wrap(err, "Couldn't create cluster")
	}

	_, err = cluster.Join([]string{clusterAddr}, true)
	if err != nil {
		log.Printf("Couldn't join cluster, starting own: %v\n", err)
	}

	return cluster, nil
}

func launchHTTPAPI(db *Value) {
	go func() {
		m := mux.NewRouter().StrictSlash(true)
		m.HandleFunc("/get", func(w http.ResponseWriter, r *http.Request) {
			val, _ := db.getValue()
			fmt.Fprintf(w, "%v", val)
		}).Methods("GET").Schemes()
		m.HandleFunc("/set/{newVal}", func(w http.ResponseWriter, r *http.Request) {
			vars := mux.Vars(r)
			newVal, err := strconv.Atoi(vars["newVal"])
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				fmt.Fprintf(w, "%v", err)
				return
			}

			db.setValue(newVal)

			fmt.Fprintf(w, "%v", newVal)
		}).Methods("POST")
		m.HandleFunc("/notify/{curVal}/{curGeneration}", func(w http.ResponseWriter, r *http.Request) {
			vars := mux.Vars(r)
			curVal, err := strconv.Atoi(vars["curVal"])
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				fmt.Fprintf(w, "%v", err)
				return
			}
			curGeneration, err := strconv.Atoi(vars["curGeneration"])
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				fmt.Fprintf(w, "%v", err)
				return
			}

			if changed := db.notifyValue(curVal, curGeneration); changed {
				log.Printf(
					"NewVal: %v Gen: %v Notifier: %v",
					curVal,
					curGeneration,
					r.URL.Query().Get("notifier"))
			}
			w.WriteHeader(http.StatusOK)
		}).Methods("GET")
		log.Fatal(http.ListenAndServe(":8080", m))
	}()
}

func getOtherMembers(cluster *serf.Serf) []serf.Member {
	members := cluster.Members()
	log.Println("Number of members ",len(members))
	for i := 0; i < len(members); {
		if members[i].Name == cluster.LocalMember().Name || members[i].Status != serf.StatusAlive {
			if i < len(members)-1 {
				members = append(members[:i], members[i + 1:]...)
			} else {
				members = members[:i]
			}
		} else {
			i++
		}
	}
	return members
}

func notifyMember(ctx context.Context, addr string, db *Value) error {
	val, gen := db.getValue()
	req, err := http.NewRequest("POST", fmt.Sprintf("http://%v:8080/notify/%v/%v?notifier=%v", addr, val, gen, ctx.Value("name")), nil)
	if err != nil {
		return errors.Wrap(err, "Couldn't create request")
	}
	req = req.WithContext(ctx)

	_, err = http.DefaultClient.Do(req)
	if err != nil {
		return errors.Wrap(err, "Couldn't make request")
	}
	return nil
}

func notifyOthers(ctx context.Context, otherMembers []serf.Member, db *Value) {
	g, ctx := errgroup.WithContext(ctx)

	if len(otherMembers) <= 2 {
		for _, member := range otherMembers {
			curMember := member
			g.Go(func() error {
				return notifyMember(ctx, curMember.Addr.String(), db)
			})
		}
	} else {
		randIndex := rand.Int() % len(otherMembers)
		for i := 0; i < membersToNotify; i++ {
			curIndex := i
			g.Go(func() error {
				return notifyMember(
					ctx,
					otherMembers[(randIndex + curIndex) % len(otherMembers)].Addr.String(),
					db)
			})
		}
	}

	err := g.Wait()
	if err != nil {
		log.Printf("Error when notifying other members: %v", err)
	}
}

func main() {
	fmt.Println("Advertise address :",os.Getenv("ADVERTISE_ADDR"),"Cluster address",os.Getenv("CLUSTER_ADDR"))
	cluster, err := setupCluster(
		"172.17.0.2",
		os.Getenv("CLUSTER_ADDR"))
	if err != nil {
		log.Fatal(err)
	}
	defer cluster.Leave()

	theOneAndOnlyNumber := initValue(42)
	launchHTTPAPI(theOneAndOnlyNumber)

	ctx := context.Background()
	if name, err := os.Hostname(); err == nil {
		ctx = context.WithValue(ctx, "name", name)
	}

	debugDataPrinterTicker := time.Tick(time.Second * 5)
	numberBroadcastTicker := time.Tick(time.Second * 2)
	for {
		select {
		case <-numberBroadcastTicker:
			members := getOtherMembers(cluster)

			ctx, _ := context.WithTimeout(ctx, time.Second*2)
			go notifyOthers(ctx, members, theOneAndOnlyNumber)

		case <-debugDataPrinterTicker:
			log.Printf("Members: %v\n", cluster.Members())

			curVal, curGen := theOneAndOnlyNumber.getValue()
			log.Printf("State: Val: %v Gen: %v\n", curVal, curGen)
		}
	}
}

