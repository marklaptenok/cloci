package main

import (
	"net"
	"os"
	"os/signal"
	"syscall"

	"codelearning.online/conf"
	"codelearning.online/logger"
)

var (
	service_waiter_channel chan bool
)

//	Signal handlers

func sigint_handler() {
	//	TO-DO: call clean_up(), delete the following
	logger.Info("SIGINT received")

	//	Stops the service.
	service_waiter_channel <- true
}

func sigterm_handler() {
	//	TO-DO: call clean_up(), delete the following
	logger.Info("SIGTERM received")

	//	Stops the service.
	service_waiter_channel <- true
}

func sigkill_handler() {
	//	TO-DO: call clean_up(), delete the following
	logger.Info("SIGKILL received")

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

			logger.Info("%v received", received_signal)

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

	logger.Info("Signal handlers were set up")
}

func init() {

	//	Checks whetner logger works or not.
	//	If not, stops the service.
	if err := logger.Check(); err != nil {
		os.Exit(int(err.(*logger.ClpError).Code))
	}

	//	TO-DO: parse command line arguments and fill the struct.
	bind_address := net.ParseIP("127.0.0.1")
	bind_port := 443
	cnf := conf.ClociConfiguration{bind_address, uint16(bind_port)}

	//	Reads service configuration from parsed command options.
	if err := conf.Read(&cnf); err != nil {
		logger.Error(err)
	}

	//	Sets handlers for SIGINT, SIGTERM, and SIGKILL signals.
	//	SIGKILL is not processed on FreeBSD.
	service_waiter_channel = make(chan bool, 1)
	set_signal_handlers(sigint_handler, sigterm_handler, sigkill_handler)

}

func main() {

	<-service_waiter_channel
	logger.Info("Service is gracefully stopped. Have a good day!")

}
