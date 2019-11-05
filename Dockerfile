FROM alpine
WORKDIR /app
COPY distApp /app/
ENTRYPOINT ["./distApp"]