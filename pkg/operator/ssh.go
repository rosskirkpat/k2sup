package ssh

import (
	"bytes"
	"io"
	"os"
	"sync"
	"fmt"

	"golang.org/x/crypto/ssh"

	scp "github.com/bramvdbogaerde/go-scp"
	
)

type SSHOperator struct {
	conn *ssh.Client
}

func (s SSHOperator) Close() error {

	return s.conn.Close()
}


func NewSSHOperator(address string, config *ssh.ClientConfig) (*SSHOperator, error) {
	conn, err := ssh.Dial("tcp", address, config)
	if err != nil {
		return nil, err
	}

	operator := SSHOperator{
		conn: conn,
	}

	return &operator, nil
}

func (s SSHOperator) CopySCP(source, target string) {
	client, err := scp.NewClientBySSH(s.conn)
	if err != nil {
		fmt.Println(err)
	}

	f, _ := os.Open(source)

	err = client.CopyFile(f, target, "0644")

	defer f.Close()
}

func (s SSHOperator) ExecuteStdio(command string, stream bool) (CommandRes, error) {

	sess, err := s.conn.NewSession()
	if err != nil {
		return CommandRes{}, err
	}

	defer sess.Close()

	sessStdOut, err := sess.StdoutPipe()
	if err != nil {
		return CommandRes{}, err
	}

	output := bytes.Buffer{}
	wg := sync.WaitGroup{}

	var stdOutWriter io.Writer
	if stream {
		stdOutWriter = io.MultiWriter(os.Stdout, &output)
	} else {
		stdOutWriter = &output
	}

	wg.Add(1)
	go func() {
		io.Copy(stdOutWriter, sessStdOut)
		wg.Done()
	}()

	sessStderr, err := sess.StderrPipe()
	if err != nil {
		return CommandRes{}, err
	}

	errorOutput := bytes.Buffer{}
	var stdErrWriter io.Writer
	if stream {
		stdErrWriter = io.MultiWriter(os.Stderr, &errorOutput)
	} else {
		stdErrWriter = &errorOutput
	}

	wg.Add(1)
	go func() {
		io.Copy(stdErrWriter, sessStderr)
		wg.Done()
	}()

	err = sess.Run(command)
	if err != nil {
		return CommandRes{}, err
	}

	wg.Wait()

	return CommandRes{
		StdErr: errorOutput.Bytes(),
		StdOut: output.Bytes(),
	}, nil
}

func (s SSHOperator) Execute(command string) (CommandRes, error) {
	return s.ExecuteStdio(command, true)
}

type CommandRes struct {
	StdOut []byte
	StdErr []byte
}

func executeCommand(cmd string) (CommandRes, error) {

	return CommandRes{}, nil
}
