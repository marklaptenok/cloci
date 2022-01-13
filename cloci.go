package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"codelearning.online/logger"
)

var (
	service_waiter_channel chan bool
)

//	Signal handlers

func sigint_handler() {
	//	TO-DO: call clean_up(), delete the following
	fmt.Println("SIGINT received")

	//	Stops the service.
	service_waiter_channel <- true
}

func sigterm_handler() {
	//	TO-DO: call clean_up(), delete the following
	fmt.Println("SIGTERM received")

	//	Stops the service.
	service_waiter_channel <- true
}

func sigkill_handler() {
	//	TO-DO: call clean_up(), delete the following
	fmt.Println("SIGKILL received")

	//	Stops the service.
	service_waiter_channel <- true
}

func set_signal_handlers(sigint_handler func(), sigterm_handler func(), sigkill_handler func()) {

	//	Initializes a channel for OS signals.
	signals_channel := make(chan os.Signal, 1)

	//	Ignores all incoming signals.
	signal.Ignore()

	//	We will process SIGINTs, SIGTERMs, and SIGKILLs only.
	//	SIGKILL will not be caught on FreeBSD. See https://pkg.go.dev/os/signal
	signal.Notify(signals_channel, syscall.SIGINT, syscall.SIGTERM, syscall.SIGKILL)

	//	Starts a signal processing routine.
	//	It reacts the same on the both signals.
	go func() {

		for received_signal := range signals_channel {

			//	TO-DO: use Logger.Log_to_stdout
			fmt.Printf("%v received\n", received_signal)

			switch received_signal {
			case syscall.SIGINT:
				sigint_handler()
			case syscall.SIGTERM:
				sigterm_handler()
			case syscall.SIGKILL:
				sigkill_handler()
			}
		}

	}()

	//	TO-DO: use Logger.Log_to_stdout
	fmt.Println("Signal handlers were set up")
}

func init() {

	logger.Check()

	service_waiter_channel = make(chan bool, 1)

	set_signal_handlers(sigint_handler, sigterm_handler, sigkill_handler)

}

func main() {

	<-service_waiter_channel
	fmt.Println("Service is gracefully stopped. Have a good day!")

}
