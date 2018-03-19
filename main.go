package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"time"

	"github.com/kelseyhightower/envconfig"
)

func main() {
	var config Config
	err := envconfig.Process("PPROF_ME", &config)
	if err != nil {
		log.Fatal(err.Error())
	}
	app := NewApp(config)

	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt)

	go app.ListenAndServe()

	<-shutdown
	fmt.Println("shutting down")

	ctx, _ := context.WithTimeout(context.Background(), 5*time.Second)
	app.Shutdown(ctx)

}
