package driver

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

const sshPort = "22"

var (
	errVMIPUnavailable  = errors.New("VM IP not available")
	errSSHDialFailed    = errors.New("SSH dial failed")
	errSSHSessionFailed = errors.New("SSH session creation failed")
)

// Exec executes an SSH command on the VM using native Go SSH client
func (c *tartCLI) Exec(ctx context.Context, config VMConfig, opts ExecOptions) (int, error) {
	if len(opts.Command) == 0 {
		return -1, errors.New("command is required but was empty")
	}

	vmName := vmName(config.Nomad.AllocID)

	ip, err := c.IPAddress(ctx, vmName, config.Driver.Network)
	if err != nil || ip == "" {
		return -1, fmt.Errorf("%w: %v", errVMIPUnavailable, err)
	}
	// SSH client config with password authentication
	sshConfig := &ssh.ClientConfig{
		User: config.Driver.SSHUser,
		Auth: []ssh.AuthMethod{
			ssh.Password(config.Driver.SSHPassword),
		},
		// TODO: Implement proper host key verification, we can probably just match the IP addresses.
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         30 * time.Second,
	}

	// Connect to SSH server
	conn, err := ssh.Dial("tcp", net.JoinHostPort(ip, sshPort), sshConfig)
	if err != nil {
		return -1, fmt.Errorf("%w: %v", errSSHDialFailed, err)
	}
	defer conn.Close()

	// Create a session
	session, err := conn.NewSession()
	if err != nil {
		return -1, fmt.Errorf("%w: %v", errSSHSessionFailed, err)
	}
	defer session.Close()

	// Set up input/output
	session.Stdin = opts.Stdin
	session.Stdout = opts.Stdout
	session.Stderr = opts.Stderr

	// Handle TTY if needed
	if opts.Tty {
		// Set up terminal modes
		modes := ssh.TerminalModes{
			ssh.ECHO:          0,     // disable echoing
			ssh.TTY_OP_ISPEED: 14400, // input speed = 14.4kbaud
			ssh.TTY_OP_OSPEED: 14400, // output speed = 14.4kbaud
		}

		// Request pseudo terminal
		if err := session.RequestPty("xterm", 40, 80, modes); err != nil {
			return -1, fmt.Errorf("request for pseudo terminal failed: %v", err)
		}

		// Handle window resizing
		if opts.ResizeCh != nil {
			go func() {
				for sz := range opts.ResizeCh {
					session.WindowChange(sz.Height, sz.Width)
				}
			}()
		}
	}

	// Run the command. Quote each argv element so paths like
	// /Volumes/My Shared Files/... survive the remote shell intact.
	cmd := shellQuoteCommand(opts.Command)
	if err := session.Run(cmd); err != nil {
		if exitErr, ok := err.(*ssh.ExitError); ok {
			return exitErr.ExitStatus(), nil
		}
		return -1, fmt.Errorf("failed to run command: %v", err)
	}

	return 0, nil
}

func shellQuoteCommand(argv []string) string {
	quoted := make([]string, len(argv))
	for i, arg := range argv {
		quoted[i] = "'" + strings.ReplaceAll(arg, "'", "'\\''") + "'"
	}
	return strings.Join(quoted, " ")
}
