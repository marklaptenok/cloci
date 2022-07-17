package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"codelearning.online/conf"
	"codelearning.online/https_server"
	"codelearning.online/logger"
)

var (
	service_waiter_channel chan bool
	https_server_handle    *https_server.Handle

	version string
)

//	Signal handlers

func sigint_handler() {
	//	TO-DO: call clean_up(), delete the following
	logger.Debug("SIGINT received")

	//	Stops the service.
	service_waiter_channel <- true
}

func sigterm_handler() {
	//	TO-DO: call clean_up(), delete the following
	logger.Debug("SIGTERM received")

	//	Stops the service.
	service_waiter_channel <- true
}

func sigkill_handler() {
	//	TO-DO: call clean_up(), delete the following
	logger.Debug("SIGKILL received")

	//	Stops the service.
	service_waiter_channel <- true
}

func set_signal_handlers(sigint_handler func(), sigterm_handler func(), sigkill_handler func()) {

	//	Initializes a channel for OS signals.
	signals_channel := make(chan os.Signal, 1)

	//	Ignores all incoming signals.
	signal.Ignore()

	//	We will process SIGINTs, SIGTERMs, SIGKILLs, and SIGCHLD only.
	//	SIGKILL will not be caught on FreeBSD. See https://pkg.go.dev/os/signal
	//	SIGCHLD is needed to correctly perform wait(6) syscalls.
	signal.Notify(signals_channel, syscall.SIGINT, syscall.SIGTERM, syscall.SIGKILL, syscall.SIGCHLD)

	//	Starts a signal processing routine.
	//	It reacts the same on the both signals.
	go func() {

		for received_signal := range signals_channel {

			logger.Debug("%v received", received_signal)

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

	//	Checks whether logger works or not.
	//	If not, stops the service.
	if err := logger.Check(); err != nil {
		os.Exit(int(err.(*logger.ClpError).Code))
	}

	//	Show current version of the service.
	//	To change it, compile with
	//	go build -ldflags "-X main.version=<major.minor.patch>"
	logger.Info("\nCodeLearning Online Compiler-Interpreter v.%s\nCopyright Mark Laptenok, 2021-2022", version)

	//	Sets handlers for SIGINT, SIGTERM, and SIGKILL signals.
	//	Note: SIGKILL is not processed on FreeBSD.
	service_waiter_channel = make(chan bool, 1)
	set_signal_handlers(sigint_handler, sigterm_handler, sigkill_handler)

	//	Reads service configuration from the command line arguments first
	//	and from the conf file the second.
	//	If reading fails, so we don't have a proper configuration, stops the service.
	var cnf *conf.ClociConfiguration
	var err error
	if cnf, err = conf.Read(os.Args[1:]); err != nil {
		logger.Error(err)
	}

	//	If we are here, than logger works and necessary OS process signals are ready for being processed,
	//	and a configuration ('cnf') is set.

	//	Allocates the processors which one by one will process clients' requests.
	processors := make([]func(ctx context.Context, message []byte) (context.Context, []byte, error), 3)
	processors[0] = parse_request
	processors[1] = build_and_run_code
	processors[2] = encode_response

	//	Starts serving of incoming requests.
	//	In case of error, stops the service.
	if https_server_handle, err = https_server.Start(cnf, processors); err != nil {
		logger.Error(err)
	}
}

func main() {

	<-service_waiter_channel

	if https_server_handle != nil {
		https_server_handle.Stop()
	}

	logger.Info("Service is gracefully stopped. Have a good day!")

}
