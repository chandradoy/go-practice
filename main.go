package onevaluedatabasewithswim

import (
	"sync"
	"context"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strconv"

	"github.com/gorilla/mux"
	"github.com/hashicorp/serf/serf"
	"github.com/pkg/errors"
	"golang.org/x/sync/errgroup
)

type Value struct {
	number     int
	generation int
	numMutex   sync.RWMutex
}

func initValue(val int) *Value {
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
	if curGeneration > n.generation {
		n.numMutex.Lock()
		defer n.numMutex.Unlock()
		n.generation = curGeneration
		n.number = curVal
		return true
	}
	return false
}
