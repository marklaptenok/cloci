package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"codelearning.online/logger"
)

type Request struct {
	Language        float64 `json:"language"`
	Code_to_process string  `json:"code_to_process"`
}

type Response struct {
	CompilerOutput string `json:"compiler_output"`
	ProgramOutput  string `json:"program_output"`
}

//	Creates the key that denotes language of the code processors face with.
//	Particular value of the code_language_key inside a context means language at which the code is written.
//	The value will be taken from the request's body by the following function (= processor).
const code_language_key int = 0

//	Creates the keys that denote position of the first byte in a set of bytes
//	which represents builder's output and position of the first byte in the set of bytes
//	which represents built application's output.
const position_compiler_output_key int = 1
const position_program_output_key int = 2

//	SUPPORT FUNCTIONS

//	Reads no more than 'limit amount of bytes from a given stream ('stream').
//	Returns error in case of an i/o error.
func read_from_stream(stream io.ReadCloser, limit int) (response []byte, summary_written_bytes_cnt int, err error) {

	response = nil
	summary_written_bytes_cnt = 0
	err = nil

	for err != io.EOF {

		var written_bytes_cnt = 0
		//	Allocates a buffer.
		read_buffer := make([]byte, limit)

		//	Reads data from the stream. Checks the state of reading.
		written_bytes_cnt, err = stream.Read(read_buffer)

		//	Removes unnecessary zero at the end of read data.
		read_buffer = bytes.Trim(read_buffer, "\x00")

		if err != nil && err != io.EOF {
			logger.Warning("Data have not been read from the stream: %s", err.Error())
			break
		}

		//	No bytes read, just stops reading.
		if written_bytes_cnt == 0 {
			break
		}

		//	Append read data to the final buffer.
		response = append(response, read_buffer...)

		//	Calculates the overall amount of bytes read from the stream.
		summary_written_bytes_cnt += written_bytes_cnt
		logger.Debug("Read %d bytes from the stream", written_bytes_cnt)

		//	Checks whether we hit the limit.
		if summary_written_bytes_cnt >= limit {
			logger.Debug("The limit reading from the stream hit (limit = %d bytes, read = %d bytes)", limit, summary_written_bytes_cnt)

			break
		}

	} //	--- end of for ---

	//	Cuts unnecessary bytes at the end.
	if summary_written_bytes_cnt > limit {
		summary_written_bytes_cnt = limit
	}
	response = response[:summary_written_bytes_cnt]

	return
}

//	PROCESSORS

func parse_request(ctx context.Context, message []byte) (context.Context, []byte, error) {

	//	If there is no given context, returns.
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
	//	The new context ('new_ctx') is the child of the previous one ('ctx').
	new_ctx := context.WithValue(ctx, code_language_key, language)

	logger.Info("Code length = %d", len(current_request.Code_to_process))
	logger.Debug("\n===== code start =====\n%s\n===== code end =====\n", current_request.Code_to_process)

	return new_ctx, []byte(current_request.Code_to_process), nil
}

func build_and_run_code(ctx context.Context, message []byte) (context.Context, []byte, error) {

	//logger.Debug("%v\n Length: %d", message, len(message))

	//	TO-DO: get these parameters from server configuration.
	child_jail_number := "3" //	TO-DO: take number of executable jail from the channel.
	dir_for_binaries := "/child_jail_" + child_jail_number
	user_binary_corename := "test"
	command_to_run := "/cloci_microservices/user_binary_runner"
	command_to_run_options := []string{child_jail_number}
	building_timeout := 500 * time.Millisecond
	running_timeout := 250 * time.Millisecond

	//	Using GCC compiler
	//	builder_path := "/usr/local/bin/gcc"
	//	builder_options := []string{"-xc", "-O0", "-std=c11", "-Wall", "-Wextra", "-pedantic", "-lm", "-"}

	//	Using clang compiler
	builder_path := "/usr/bin/clang"
	builder_options := []string{"-xc", "-O0", "-std=c11", "-Wall", "-Wextra", "-pedantic", "-"}

	stderr_buffer_size := (1 << 11) //	2Kb
	stdout_buffer_size := (1 << 11) //	2Kb

	//	If no message (= code to build) presented or no given context, returns.
	if message == nil || len(message) == 0 || ctx == nil {
		location, _ := logger.Get_function_name()
		return ctx, nil, &logger.ClpError{
			Code:     1,
			Msg:      "Code to build or/and context is not presented",
			Location: location}
	}

	//	Checks crutial input arguments.
	if stderr_buffer_size <= 0 || stdout_buffer_size <= 0 || building_timeout == 0 || running_timeout == 0 {
		location, _ := logger.Get_function_name()
		return ctx, nil, &logger.ClpError{
			Code:     100,
			Msg:      "Invalid limits",
			Location: location}
	}

	//	Creates a temporary file in /tmp to write builder's output if building will be successful.
	//	TO-DO: dir_for_binaries has to be specified dynamically, depending on currently free runtime jail.
	output_file, err := os.CreateTemp(dir_for_binaries, user_binary_corename)
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

	logger.Debug("The output file %s for the binary is ready.", output_file.Name())

	// Adds filename for the output binary to the builder options.
	builder_options = append(builder_options, "-o", output_file.Name())

	//	Takes from the given context the language in which the code is written.
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

			written_bytes_cnt, err := stdin.Write(message)
			if err != nil || written_bytes_cnt != len(message) {
				logger.Warning("Code of length = %d bytes (sent = %d bytes) were not sent to the builder: %v", len(message), written_bytes_cnt, err)
			} else {
				logger.Debug("Builder is working. %d bytes were passed to it", written_bytes_cnt)
			}

		}()

		//	Reads from stderr the certain amount of bytes. Warning! It's not accurate.
		//	If any error happens during building, the builder writes here.
		builder_stderr_response, summary_written_bytes_cnt, stderr_reader_err := read_from_stream(stderr, stderr_buffer_size)
		logger.Debug("Building has completed. %d bytes were read from stderr", len(builder_stderr_response))
		if summary_written_bytes_cnt != len(builder_stderr_response) {
			logger.Warning("Reading from builder's stderr has been completed with error")
		}

		logger.Debug("\n===== (Raw output) builder stderr response start =====\n%v\n===== builder stderr response end =====\n", builder_stderr_response)

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

		//	Removes unnecessary symbols and phrases from the builder's output.
		builder_stderr_response = bytes.Trim(bytes.ReplaceAll(builder_stderr_response, []byte("<stdin>:"), []byte("")), "\n\x00 ")
		//builder_stderr_response = bytes.ReplaceAll(builder_stderr_response, []byte("/usr/local/bin/ld"), []byte("ld (linker)"))
		//builder_stderr_response = bytes.ReplaceAll(builder_stderr_response, []byte("/tmp/"), []byte(""))

		//	Checks whether the builder reports any error/warning.
		//	If so returns its output only without execution of the built code.
		if len(builder_stderr_response) > 0 {
			logger.Debug("Builder has reported errors\n===== builder stderr response start =====\n%s\n===== builder stderr response end =====\n", string(builder_stderr_response))

			//	Puts the positions to the given context ('ctx').
			//	The new context ('new_ctx') is the child of the previous one ('ctx').
			new_ctx := context.WithValue(ctx, position_compiler_output_key, 0)
			//	We have only builder's output, so sets the position
			//	from which application's output begins to the end of whole message.
			new_ctx = context.WithValue(new_ctx, position_program_output_key, len(builder_stderr_response))

			return new_ctx, builder_stderr_response, nil
		}

		//	If the builder reports no error, we will execute the built application.

		logger.Info("Builder successfully finished. Binary: %s", output_file.Name())

		//	Prepares a context with the timeout for running the built application.
		ctx_run_output, cancel_run_output := context.WithTimeout(ctx, running_timeout)
		defer cancel_run_output()

		//	We know by now the name of the temporary binary, so we are preparing its launch
		command_to_run_options = append(command_to_run_options, "/"+filepath.Base(output_file.Name()))

		//	Prepares an object to run the application.
		//	TO-DO: make it possible the built application to receive CLI arguments.
		cmd_run_output := exec.CommandContext(ctx_run_output, command_to_run, command_to_run_options...)

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

		//	Reads application's output to stdout.
		code_stdout_response, stdout_response_size, stdout_reader_err := read_from_stream(stdout, stdout_buffer_size)
		if stdout_reader_err != nil && stdout_reader_err != io.EOF {
			logger.Warning("Stdout response from the executed application was not read: %s", stdout_reader_err.Error())
		}
		logger.Info("Application has written %d bytes to stdout", stdout_response_size)

		//	Reads application's output to stderr.
		code_stderr_response, stderr_response_size, stderr_reader_err := read_from_stream(stderr, stderr_buffer_size)
		if stderr_reader_err != nil && stderr_reader_err != io.EOF {
			logger.Warning("Stderr response from the executed application was not read: %s", stderr_reader_err.Error())
		}
		logger.Info("Application has written %d bytes to stderr", stderr_response_size)

		//	Waits until the application exited and all data will be read from stdout and stderr.
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

		logger.Debug("\n===== application stdout response start (length = %d bytes)=====\n%s\n===== application stdout response end =====\n",
			len(code_stdout_response), string(code_stdout_response))
		logger.Debug("\n===== application stderr response start (length = %d bytes)=====\n%s\n===== application stderr response end =====\n",
			len(code_stderr_response), string(code_stderr_response))

		//	Prepares the result (combines the bytes taken from stdout and stderr of the exited application).
		result := []byte{}
		if len(code_stdout_response) > 0 {
			result = append(result, code_stdout_response...)
		}
		if len(code_stderr_response) > 0 {
			if len(code_stdout_response) > 0 {
				result = append(result, '\n', '\n')
			}
			result = append(result, code_stderr_response...)
		}
		logger.Debug("Build-and-run result length = %d bytes", len(result))

		//	Puts the positions to the given context ('ctx').
		//	The new context ('new_ctx') is the child of the previous one ('ctx').

		//	We have only application's output, so sets the position
		//	from which application's output begins to the end of whole message.
		new_ctx := context.WithValue(ctx, position_compiler_output_key, len(result))
		//	We have only application's output, so sets the position
		//	from which application's output begins to 0.
		new_ctx = context.WithValue(new_ctx, position_program_output_key, 0)

		return new_ctx, result, nil

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

func encode_response(ctx context.Context, message []byte) (context.Context, []byte, error) {

	//	If no message (= response to pack) presented or no given context, returns.
	if message == nil || ctx == nil {
		location, _ := logger.Get_function_name()
		return ctx, nil, &logger.ClpError{
			Code:     1,
			Msg:      "Code to build or/and context is not presented",
			Location: location}
	}

	//	Checks that the context has not been cancelled.
	select {
	case <-ctx.Done():
		return ctx, nil, ctx.Err()
	default:
	}

	//	Takes from the given context the positions of builder's and application's outputs.

	position_compiler_output, is_int := ctx.Value(position_compiler_output_key).(int)
	//	Checks whether it's of proper type or not.
	if !is_int {
		location, _ := logger.Get_function_name()
		return ctx, nil, &logger.ClpError{
			Code:     2,
			Msg:      "Can't get the position of compiler's output: the context has wrong data",
			Location: location}
	}

	position_application_output, is_int := ctx.Value(position_program_output_key).(int)
	//	Checks whether it's of proper type or not.
	if !is_int {
		location, _ := logger.Get_function_name()
		return ctx, nil, &logger.ClpError{
			Code:     3,
			Msg:      "Can't get the position of application's output: the context has wrong data",
			Location: location}
	}

	//	Parses out builder's output and built program's output from the 'message'.
	//	'message' looks like <bytes_that_represent_compiler_output><bytes_that_represent_application_output> or
	//	<bytes_that_represent_application_output><bytes_that_represent_compiler_output>
	var compiler_output []byte = nil
	var program_output []byte = nil

	//	If any of the outputs is not empty, parses out proper JSON fileds from the 'message'.
	if position_compiler_output != 0 || position_application_output != 0 {

		if position_compiler_output < position_application_output {
			compiler_output = json.RawMessage(message[position_compiler_output:position_application_output])
			program_output = json.RawMessage(message[position_application_output:])
		} else {
			compiler_output = json.RawMessage(message[position_compiler_output:])
			program_output = json.RawMessage(message[position_application_output:position_compiler_output])
		}

	}

	//	Creates a JSON formatted object for further marshaling.
	response_object := Response{CompilerOutput: string(compiler_output), ProgramOutput: string(program_output)}

	//TO-DO: write a function to mask special characters including '"', '\n', '\t' and replace marshaliser

	//	Creates a set of byte that represents a JSON formatted string.
	response, err := json.MarshalIndent(&response_object, "", "\t")
	if err != nil {
		location, _ := logger.Get_function_name()
		return ctx, nil, &logger.ClpError{
			Code:     4,
			Msg:      "Can't pack a JSON string: " + err.Error(),
			Location: location}
	}

	//	Masks special characters ('\n', '\t', '"').
	response = bytes.ReplaceAll(response, []byte("\\n"), []byte("\\\\n"))
	response = bytes.ReplaceAll(response, []byte("\\t"), []byte("\\\\t"))
	//TO-DO: write a function to mask special characters including '"', '\n', '\t' and replace marshaliser
	//response = bytes.ReplaceAll(response, []byte("\""), []byte("\\\""))

	return ctx, response, nil
}
