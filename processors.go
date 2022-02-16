package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"

	"codelearning.online/logger"
)

type Request struct {
	Language        float64 `json:"language"`
	Code_to_process string  `json:"code_to_process"`
}

//	Creates an alias for the integer type to use it as a key in contexts
//	that are passed throw the following processors.
type language_key int

//	Creates the key that denotes language of the code processors face with.
//	Particular value of the code_language_key inside a context means language at which the code is written.
//	The value will be taken from the request's body by the following function (= processor).
const code_language_key language_key = 0

func parse_request(ctx context.Context, message []byte) (context.Context, []byte, error) {

	//	If no no given context, returns.
	if ctx == nil {
		location, _ := logger.Get_function_name()
		return ctx, nil, &logger.ClpError{
			Code:     1,
			Msg:      "Context is not presented",
			Location: location}
	}

	//	Allocates space for data being processed.
	var current_request Request

	logger.Debug("Unmarshaling the request...")

	//	Tries to patse the JSON.
	//	In case of any error, returns it.
	if err := json.Unmarshal(message, &current_request); err != nil {
		location, _ := logger.Get_function_name()
		return ctx, nil, &logger.ClpError{
			Code:     2,
			Msg:      "Unknown request structure: " + err.Error(),
			Location: location}
	}

	logger.Debug("Taking the code to build out of the request...")

	//	If code to build is not presented, returns.
	if len(current_request.Code_to_process) == 0 {
		location, _ := logger.Get_function_name()
		return ctx, nil, &logger.ClpError{
			Code:     3,
			Msg:      "Code is empty",
			Location: location}
	}

	logger.Debug("Taking the language of the request...")

	//	Parses out the language the code is written.
	var language uint8
	switch current_request.Language {
	case 0:
		language = 0
	case 1:
		language = 1
	//	If language is not recognised, returns.
	default:
		location, _ := logger.Get_function_name()
		return ctx, nil, &logger.ClpError{
			Code:     4,
			Msg:      fmt.Sprintf("Language is unknown (%d)", uint8(current_request.Language)),
			Location: location}
	}

	logger.Info("Language = %d", language)

	//	Puts the language to the given context ('ctx').
	//	The new context ('new_ctx') is the child of the precious one ('ctx').
	new_ctx := context.WithValue(ctx, code_language_key, language)

	logger.Info("Code length = %d", len(current_request.Code_to_process))
	logger.Debug("\n===== code start =====\n%s\n===== code end =====\n", current_request.Code_to_process)

	return new_ctx, []byte(current_request.Code_to_process), nil
}

func build_and_run_code(ctx context.Context, message []byte) (context.Context, []byte, error) {

	//	TO-DO: get these parameters from server configuration.
	dir_for_binaries := ""
	building_timeout := 250 * time.Millisecond
	running_timeout := 250 * time.Millisecond
	builder_path := "/usr/local/bin/gcc"
	builder_options := []string{"-xc", "-O0", "-std=c11", "-Wall", "-Wextra", "-pedantic", "-", "-o"}
	//	TO-DO: check. Has to be greater than 0
	stderr_buffer_size := 20        //(1 << 11) //	2Kb
	stdout_buffer_size := (1 << 11) //	2Kb

	//	If no message (= code to build) presented or no given context, returns.
	if message == nil || len(message) == 0 || ctx == nil {
		location, _ := logger.Get_function_name()
		return ctx, nil, &logger.ClpError{
			Code:     1,
			Msg:      "Code to build or/and context is not presented",
			Location: location}
	}

	//	Creates a temporary file in /tmp to write builder's output if building will be successful.
	//	TO-DO: specify a directory which is alloacted on a tmpfs
	output_file, err := os.CreateTemp(dir_for_binaries, "cloci")
	if err != nil {
		location, _ := logger.Get_function_name()
		return ctx, nil, &logger.ClpError{
			Code:     2,
			Msg:      "Can't create a temporary file to write builder's output: " + err.Error(),
			Location: location}
	}
	//	This is needed to automatically delete the temporary file when this function returns.
	defer os.Remove(output_file.Name())

	//	We need to close file, so the builder can write to it.
	if err = output_file.Close(); err != nil {
		location, _ := logger.Get_function_name()
		return ctx, nil, &logger.ClpError{
			Code:     3,
			Msg:      "Can't prepare a temporary file to write builder's output: " + err.Error(),
			Location: location}
	}

	// Adds filename for the output binary to the builder options.
	builder_options = append(builder_options, output_file.Name())

	//	Take from the given context the language in which the code is written.
	language, is_uint8 := ctx.Value(code_language_key).(uint8)
	//	Checks whether it's of proper type or not.
	if !is_uint8 {
		location, _ := logger.Get_function_name()
		return ctx, nil, &logger.ClpError{
			Code:     4,
			Msg:      "Can't determine the language given code is written in: the context has wrong data",
			Location: location}
	}

	switch language {
	//	Compiles the C code.
	case 0:

		//	Prepares the timeout for building to complete.
		//	If the connection (which invokes this processor) will be closed (e.g by any client's reason),
		//	the new context will be canceled, so building will be aborted.
		//	TO-DO: investigate what if context will be canceled before the command starts or before it writes to stderr.
		ctx_building, cancel_func := context.WithTimeout(ctx, building_timeout)
		defer cancel_func()

		//	Prepares the command to build text taken from stdin.
		cmd_building := exec.CommandContext(ctx_building, builder_path, builder_options...)

		//	Connects to the stdin and stderr. gcc doesn't write to stdout usually.

		stdin, err := cmd_building.StdinPipe()
		if err != nil {
			location, _ := logger.Get_function_name()
			return ctx, nil, &logger.ClpError{
				Code:     5,
				Msg:      "Can't connect to stdin: " + err.Error(),
				Location: location}
		}

		stderr, err := cmd_building.StderrPipe()
		if err != nil {
			location, _ := logger.Get_function_name()
			return ctx, nil, &logger.ClpError{
				Code:     6,
				Msg:      "Can't connect to stderr: " + err.Error(),
				Location: location}
		}

		//	Executes the builder.
		if err = cmd_building.Start(); err != nil {
			location, _ := logger.Get_function_name()
			return ctx, nil, &logger.ClpError{
				Code:     7,
				Msg:      "Can't start the builder: " + err.Error(),
				Location: location}
		}

		//	Writes the code ('message') to the stdin, so the builder can read it.
		go func() {

			defer stdin.Close()

			written_bytes_cnt, err := io.WriteString(stdin, string(message))
			if err != nil || written_bytes_cnt != len(message) {
				logger.Warning("Code of length = %d bytes (sent = %d bytes) was not sent to the builder: %v", len(message), written_bytes_cnt, err)
			} else {
				logger.Debug("Building in progress. %d bytes passed to it", written_bytes_cnt)
			}

		}()

		//	Reads from stderr the certain amount of bytes. Wanring! It's not accurate.
		//	If any error happens during building, the builder writes here.
		var builder_stderr_response []byte
		var stderr_reader_err error
		var summary_written_bytes_cnt = 0
		for stderr_reader_err != io.EOF {

			var written_bytes_cnt = 0
			read_buffer := make([]byte, stderr_buffer_size)

			written_bytes_cnt, stderr_reader_err = stderr.Read(read_buffer)
			if stderr_reader_err != nil && stderr_reader_err != io.EOF {
				logger.Warning("Stderr response from the builder was not read: %s", stderr_reader_err.Error())
				break
			}

			//	No bytes read, just stops reading.
			if written_bytes_cnt == 0 {
				break
			}

			builder_stderr_response = append(builder_stderr_response, read_buffer...)

			//	Calculates the overall amount of bytes read from builder's stderr.
			summary_written_bytes_cnt += written_bytes_cnt
			logger.Debug("Read %d bytes from stderr response from the builder", written_bytes_cnt)

			//	Checks whether we hit the limit.
			if summary_written_bytes_cnt >= stderr_buffer_size {
				logger.Debug("The limit reading from builder's stderr hit (limit = %d bytes, read = %d bytes)", stderr_buffer_size, summary_written_bytes_cnt)

				builder_stderr_response = builder_stderr_response[:stderr_buffer_size-1]
				break
			}

		}
		logger.Debug("Building finished. %d bytes read from stderr", summary_written_bytes_cnt)

		//	Waits until the builder stopped and all data will be read from stderr.
		//	Closes the stderr pipe and deallocates resources for 'cmd_building'.
		if err = cmd_building.Wait(); err != nil {

			switch err.(type) {
			case *exec.ExitError:
				switch err.(*exec.ExitError).ExitCode() {
				//	If the linker or any of parts of gcc reports an error, ignores it.
				//	It will be processed further.
				case 1:
					break
				//	If the building timeout hit, returns.
				case -1:
					location, _ := logger.Get_function_name()
					return ctx, nil, &logger.ClpError{
						Code:     8,
						Msg:      fmt.Sprintf("Timeout %v for building the code hit", building_timeout),
						Location: location}
				default:
					location, _ := logger.Get_function_name()
					return ctx, nil, &logger.ClpError{
						Code:     9,
						Msg:      "Building failed: " + err.Error(),
						Location: location}
				}
			default:
				location, _ := logger.Get_function_name()
				return ctx, nil, &logger.ClpError{
					Code:     10,
					Msg:      "Building failed: " + err.Error(),
					Location: location}
			}

			logger.Debug("Builder has terminated with error: %s", err.Error())
		}

		//	Checks if we can progress with execution of builder's output (= a built application).
		if stderr_reader_err != nil && stderr_reader_err != io.EOF {
			location, _ := logger.Get_function_name()
			return ctx, nil, &logger.ClpError{
				Code:     11,
				Msg:      "Builder can't write to stderr",
				Location: location}
		}

		//	Removes unnecessary symbols from the builder's output.
		builder_stderr_response = bytes.Trim(bytes.ReplaceAll(builder_stderr_response, []byte("<stdin>:"), []byte("")), "\n\x00 ")

		//	Checks whether the builder reports any error/warning.
		//	If so returns its output only without execution of the built code.
		if len(builder_stderr_response) > 0 {
			logger.Debug("Builder has reported errors\n===== builder stderr response start =====\n%s\n===== builder stderr response end =====\n", string(builder_stderr_response))

			return ctx, builder_stderr_response, nil
		}

		//	If the builder reports no error, we will execute the built application.

		logger.Info("Builder successfully finished")

		//	Prepares a context with the timeout for running the built application.
		ctx_run_output, cancel_run_output := context.WithTimeout(ctx, running_timeout)
		defer cancel_run_output()

		//	Prepares an object to run the application.
		//	TO-DO: this has to be run in a jail
		//	TO-DO: make it possible the built application to receive CLI arguments.
		cmd_run_output := exec.CommandContext(ctx_run_output, output_file.Name())

		//	Prepares stdout to read output from running application.
		stdout, err := cmd_run_output.StdoutPipe()
		if err != nil {
			location, _ := logger.Get_function_name()
			return ctx, nil, &logger.ClpError{
				Code:     12,
				Msg:      "Can't connect to stdout: " + err.Error(),
				Location: location}
		}

		stderr, err = cmd_run_output.StderrPipe()
		if err != nil {
			location, _ := logger.Get_function_name()
			return ctx, nil, &logger.ClpError{
				Code:     13,
				Msg:      "Can't connect to stderr: " + err.Error(),
				Location: location}
		}

		//	Executes the built application.
		if err = cmd_run_output.Start(); err != nil {
			location, _ := logger.Get_function_name()
			return ctx, nil, &logger.ClpError{
				Code:     14,
				Msg:      "Can't start the built application: " + err.Error(),
				Location: location}
		}

		code_stdout_response := make([]byte, stdout_buffer_size)
		_, stdout_reader_err := stdout.Read(code_stdout_response)
		if stdout_reader_err != nil && stdout_reader_err != io.EOF {
			logger.Warning("Stdout response from the executed application was not read: %s", stdout_reader_err.Error())
		}

		code_stderr_response := make([]byte, stderr_buffer_size)
		_, stderr_reader_err = stdout.Read(code_stderr_response)
		if stderr_reader_err != nil && stderr_reader_err != io.EOF {
			logger.Warning("Stderr response from the executed application was not read: %s", stderr_reader_err.Error())
		}

		//	Waits until the application stopped and all data will be read from stdout and stderr.
		//	Closes the stdout and stderr pipes and deallocates resources for 'cmd_run_output'.
		if err = cmd_run_output.Wait(); err != nil {

			switch err.(type) {
			case *exec.ExitError:
				switch err.(*exec.ExitError).ExitCode() {
				//	If running timeout hit, returns.
				case -1:
					location, _ := logger.Get_function_name()
					return ctx, nil, &logger.ClpError{
						Code:     15,
						Msg:      fmt.Sprintf("Timeout %v for running the code hit", running_timeout),
						Location: location}
				default:
					logger.Info("The application exits with code %s", err.Error())
				}
			default:
				location, _ := logger.Get_function_name()
				return ctx, nil, &logger.ClpError{
					Code:     16,
					Msg:      "Running the application failed: " + err.Error(),
					Location: location}
			}
		}

		logger.Debug("\n===== application stdout response start =====\n%s\n===== application stdout response end =====\n", string(code_stdout_response))
		logger.Debug("\n===== application stderr response start =====\n%s\n===== application stderr response end =====\n", string(code_stderr_response))

		//	Prepares the result (combined bytes taken from stdout and stderr of the exited application).
		var result []byte = nil
		result = append(result, code_stdout_response...)
		result = append(result, '\n', '\n')
		result = append(result, code_stderr_response...)

		return ctx, result, nil

	//	Compiles the C++ code.
	case 1:
		//	TO-DO: call g++ like in the previous case.
	//	If language is not recognised, returns.
	default:
		location, _ := logger.Get_function_name()
		return ctx, nil, &logger.ClpError{
			Code:     17,
			Msg:      fmt.Sprintf("Language is unknown (%d)", language),
			Location: location}
	}

	//	If no processing of original message ('message') has happened, pass it as it is.
	return ctx, message, nil
}
