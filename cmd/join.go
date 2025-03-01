package cmd

import (
	"fmt"
	"net"
	"os"
	"runtime"
	"strings"

	operator "github.com/alexellis/k3sup/pkg/operator"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"golang.org/x/crypto/ssh"
)

// SupportMsg is aimed to inform the many hundreds of users of k3sup
// that they can do their part to support the project's development
// and maintenance.
const SupportMsg = `Give your support to k3sup via GitHub Sponsors:

https://github.com/sponsors/alexellis`

// MakeJoin creates the join command
func MakeJoin() *cobra.Command {
	var command = &cobra.Command{
		Use:   "join",
		Short: "Install the RKE2 agent on a remote host and join it to an existing server",
		Long: `Install the RKE2 agent on a remote host and join it to an existing server

` + SupportMsg,
		Example: `  k2sup join --user root --server-ip IP --ip IP

  k2sup join --user pi \
    --server-host HOST \
    --host HOST \
    --channel latest`,
		SilenceUsage: true,
	}

	command.Flags().IP("ip", net.ParseIP("127.0.0.1"), "Public IP of node on which to install agent")
	command.Flags().IP("server-ip", net.ParseIP("127.0.0.1"), "Public IP of an existing RKE2 server")

	command.Flags().String("host", "", "Public hostname of node on which to install agent")
	command.Flags().String("server-host", "", "Public hostname of an existing RKE2 server")

	command.Flags().String("user", "root", "Username for SSH login")
	command.Flags().String("server-user", "root", "Server username for SSH login (Default to --user)")

	command.Flags().String("ssh-key", "~/.ssh/id_rsa", "The ssh key to use for remote login")
	command.Flags().Int("ssh-port", 22, "The port on which to connect for ssh")
	command.Flags().Int("server-ssh-port", 22, "The port on which to connect to server for ssh (Default to --ssh-port)")
	command.Flags().Bool("skip-install", false, "Skip the RKE2 installer")
	command.Flags().Bool("sudo", true, "Use sudo for installation. e.g. set to false when using the root user and no sudo is available.")

	command.Flags().Bool("server", false, "Join the cluster as a server rather than as an agent for the embedded etcd mode")
	command.Flags().Bool("print-command", false, "Print a command that you can use with SSH to manually recover from an error")

	command.Flags().String("version", "", "Set a version to install, overrides --channel")
	command.Flags().String("channel", PinnedChannel, "Release channel: stable, latest, or i.e. v1.19")
	command.Flags().String("config", "", "RKE2 configuration file to use")
	command.Flags().String("registries", "", "containerd registry configuration file to use")

	command.RunE = func(command *cobra.Command, args []string) error {
		fmt.Printf("Running: k2sup join\n")

		ip, err := command.Flags().GetIP("ip")
		if err != nil {
			return err
		}

		host, err := command.Flags().GetString("host")
		if err != nil {
			return err
		}
		if len(host) == 0 {
			host = ip.String()
		}

		serverIP, err := command.Flags().GetIP("server-ip")
		if err != nil {
			return err
		}

		serverHost, err := command.Flags().GetString("server-host")
		if err != nil {
			return err
		}
		if len(serverHost) == 0 {
			serverHost = serverIP.String()
		}

		fmt.Println("Server IP: " + serverHost)

		user, _ := command.Flags().GetString("user")

		serverUser := user
		if command.Flags().Changed("server-user") {
			serverUser, _ = command.Flags().GetString("server-user")
		}

		sshKey, _ := command.Flags().GetString("ssh-key")
		server, err := command.Flags().GetBool("server")
		if err != nil {
			return err
		}

		port, _ := command.Flags().GetInt("ssh-port")
		serverPort := port
		if command.Flags().Changed("server-ssh-port") {
			serverPort, _ = command.Flags().GetInt("server-ssh-port")
		}

		rke2Version, err := command.Flags().GetString("version")
		if err != nil {
			return err
		}
		rke2Channel, err := command.Flags().GetString("channel")
		if err != nil {
			return err
		}

		configFile, err := command.Flags().GetString("config")
		if err != nil {
			return err
		}

		registriesFile, err := command.Flags().GetString("registries")
		if err != nil {
			return err
		}

		if len(rke2Version) == 0 && len(rke2Channel) == 0 {
			return fmt.Errorf("give a value for --version or --channel")
		}

		printCommand, err := command.Flags().GetBool("print-command")
		if err != nil {
			return err
		}

		useSudo, err := command.Flags().GetBool("sudo")
		if err != nil {
			return err
		}
		sudoPrefix := ""
		if useSudo {
			sudoPrefix = "sudo "
		}

		sshKeyPath := expandPath(sshKey)
		address := fmt.Sprintf("%s:%d", serverHost, serverPort)

		var sshOperator *operator.SSHOperator
		var initialSSHErr error
		if runtime.GOOS != "windows" {

			var sshAgentAuthMethod ssh.AuthMethod
			sshAgentAuthMethod, initialSSHErr = sshAgentOnly()
			if initialSSHErr == nil {
				// Try SSH agent without parsing key files, will succeed if the user
				// has already added a key to the SSH Agent, or if using a configured
				// smartcard
				config := &ssh.ClientConfig{
					User:            serverUser,
					Auth:            []ssh.AuthMethod{sshAgentAuthMethod},
					HostKeyCallback: ssh.InsecureIgnoreHostKey(),
				}

				sshOperator, initialSSHErr = operator.NewSSHOperator(address, config)
			}
		} else {
			initialSSHErr = errors.New("ssh-agent unsupported on windows")
		}

		// If the initial connection attempt fails fall through to the using
		// the supplied/default private key file
		var publicKeyFileAuth ssh.AuthMethod
		var closeSSHAgent func() error
		if initialSSHErr != nil {
			var err error
			publicKeyFileAuth, closeSSHAgent, err = loadPublickey(sshKeyPath)
			if err != nil {
				return errors.Wrapf(err, "unable to load the ssh key with path %q", sshKeyPath)
			}

			defer closeSSHAgent()

			config := &ssh.ClientConfig{
				User: serverUser,
				Auth: []ssh.AuthMethod{
					publicKeyFileAuth,
				},
				HostKeyCallback: ssh.InsecureIgnoreHostKey(),
			}

			sshOperator, err = operator.NewSSHOperator(address, config)

			if err != nil {
				return errors.Wrapf(err, "unable to connect to (server) %s over ssh", address)
			}
		}

		defer sshOperator.Close()

		getTokenCommand := fmt.Sprintf(sudoPrefix + "cat /var/lib/rancher/rke2/server/node-token\n")
		if printCommand {
			fmt.Printf("ssh: %s\n", getTokenCommand)
		}

		res, err := sshOperator.Execute(getTokenCommand)

		if err != nil {
			return errors.Wrap(err, "unable to get join-token from server")
		}

		if len(res.StdErr) > 0 {
			fmt.Printf("Logs: %s", res.StdErr)
		}

		if closeSSHAgent != nil {
			closeSSHAgent()
		}
		sshOperator.Close()

		joinToken := string(res.StdOut)

		var boostrapErr error
		if server {
			boostrapErr = setupAdditionalServer(serverHost, host, port, user, sshKeyPath, joinToken, rke2Version, rke2Channel, configFile, registriesFile, sudoPrefix, printCommand)
		} else {
			boostrapErr = setupAgent(serverHost, host, port, user, sshKeyPath, joinToken, rke2Version, rke2Channel, configFile, registriesFile, sudoPrefix, printCommand)
		}

		return boostrapErr
	}

	command.PreRunE = func(command *cobra.Command, args []string) error {
		_, err := command.Flags().GetIP("ip")
		if err != nil {
			return err
		}
		_, err = command.Flags().GetIP("server-ip")
		if err != nil {
			return err
		}
		_, err = command.Flags().GetString("host")
		if err != nil {
			return err
		}
		_, err = command.Flags().GetString("server-host")
		if err != nil {
			return err
		}
		_, err = command.Flags().GetInt("ssh-port")
		if err != nil {
			return err
		}
		return nil
	}

	return command
}

func setupAdditionalServer(serverHost, host string, port int, user, sshKeyPath, joinToken, rke2Version, rke2Channel, configFile, registriesFile, sudoPrefix string, printCommand bool) error {
	address := fmt.Sprintf("%s:%d", host, port)

	var sshOperator *operator.SSHOperator
	var initialSSHErr error
	if runtime.GOOS != "windows" {

		var sshAgentAuthMethod ssh.AuthMethod
		sshAgentAuthMethod, initialSSHErr = sshAgentOnly()
		if initialSSHErr == nil {
			// Try SSH agent without parsing key files, will succeed if the user
			// has already added a key to the SSH Agent, or if using a configured
			// smartcard
			config := &ssh.ClientConfig{
				User:            user,
				Auth:            []ssh.AuthMethod{sshAgentAuthMethod},
				HostKeyCallback: ssh.InsecureIgnoreHostKey(),
			}

			sshOperator, initialSSHErr = operator.NewSSHOperator(address, config)
		}
	} else {
		initialSSHErr = errors.New("ssh-agent unsupported on windows")
	}

	// If the initial connection attempt fails fall through to the using
	// the supplied/default private key file
	if initialSSHErr != nil {
		publicKeyFileAuth, closeSSHAgent, err := loadPublickey(sshKeyPath)
		if err != nil {
			return errors.Wrapf(err, "unable to load the ssh key with path %q", sshKeyPath)
		}

		defer closeSSHAgent()

		config := &ssh.ClientConfig{
			User: user,
			Auth: []ssh.AuthMethod{
				publicKeyFileAuth,
			},
			HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		}

		sshOperator, err = operator.NewSSHOperator(address, config)

		if err != nil {
			return errors.Wrapf(err, "unable to connect to %s over ssh as %s", address, user)
		}
	}

	installStr := createVersionStr(rke2Version, rke2Channel)

	fmt.Println(installStr)

	defer sshOperator.Close()

	sshOperator.Execute(fmt.Sprintf("%s mkdir -p "+rke2ConfigPath, sudoPrefix))

	if configFile != "" {
		f, err := os.Open(configFile)
		if err != nil {
			return errors.Wrapf(err, "unable to open specified config file %q", configFile)
		}
		defer f.Close()
		sshOperator.CopySCP(f, rke2ConfigFile)
	}

	if registriesFile != "" {
		f, err := os.Open(registriesFile)
		if err != nil {
			return errors.Wrapf(err, "unable to open specified config file %q", registriesFile)
		}
		defer f.Close()
		sshOperator.CopySCP(f, containerdRegistriesFile)
	}

	installRKE2Exec := installStr + " INSTALL_RKE2_TYPE='server' sh -s -"

	rkeConfig := makeConfig(serverHost, strings.TrimSpace(joinToken))

	populateConfig := fmt.Sprintf("echo '%s' | %s tee -a "+rke2ConfigFile, rkeConfig, sudoPrefix)
	installAgentServerCommand := fmt.Sprintf("%s | %s %s", getScript, sudoPrefix, installRKE2Exec)
	ensureSystemdcommand := fmt.Sprintf("%s systemctl enable --no-block --now rke2-server", sudoPrefix)

	if printCommand {
		fmt.Printf("ssh: %s\n", installAgentServerCommand)
	}

	_, err := sshOperator.Execute(populateConfig)
	if err != nil {
		return err
	}

	res, err := sshOperator.Execute(installAgentServerCommand)
	if err != nil {
		return errors.Wrap(err, "unable to setup agent")
	}

	fmt.Printf("🐌 Joining server node to cluster, please wait while services start...\n")
	_, err = sshOperator.Execute(ensureSystemdcommand)
	if err != nil {
		return err
	}

	if len(res.StdErr) > 0 {
		fmt.Printf("Logs: %s", res.StdErr)
	}

	joinRes := string(res.StdOut)
	fmt.Printf("Output: %s", string(joinRes))

	return nil
}

func setupAgent(serverHost, host string, port int, user, sshKeyPath, joinToken, rke2Version, rke2Channel, configFile, registriesFile, sudoPrefix string, printCommand bool) error {

	address := fmt.Sprintf("%s:%d", host, port)

	var sshOperator *operator.SSHOperator
	var initialSSHErr error
	if runtime.GOOS != "windows" {

		var sshAgentAuthMethod ssh.AuthMethod
		sshAgentAuthMethod, initialSSHErr = sshAgentOnly()
		if initialSSHErr == nil {
			// Try SSH agent without parsing key files, will succeed if the user
			// has already added a key to the SSH Agent, or if using a configured
			// smartcard
			config := &ssh.ClientConfig{
				User:            user,
				Auth:            []ssh.AuthMethod{sshAgentAuthMethod},
				HostKeyCallback: ssh.InsecureIgnoreHostKey(),
			}

			sshOperator, initialSSHErr = operator.NewSSHOperator(address, config)
		}
	} else {
		initialSSHErr = errors.New("ssh-agent unsupported on windows")
	}

	// If the initial connection attempt fails fall through to the using
	// the supplied/default private key file
	if initialSSHErr != nil {
		publicKeyFileAuth, closeSSHAgent, err := loadPublickey(sshKeyPath)
		if err != nil {
			return errors.Wrapf(err, "unable to load the ssh key with path %q", sshKeyPath)
		}

		defer closeSSHAgent()

		config := &ssh.ClientConfig{
			User: user,
			Auth: []ssh.AuthMethod{
				publicKeyFileAuth,
			},
			HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		}

		sshOperator, err = operator.NewSSHOperator(address, config)

		if err != nil {
			return errors.Wrapf(err, "unable to connect to %s over ssh", address)
		}
	}

	defer sshOperator.Close()

	sshOperator.Execute(fmt.Sprintf("%s mkdir -p " + rke2ConfigPath, sudoPrefix))

	if configFile != "" {
		f, err := os.Open(configFile)
		if err != nil {
			return errors.Wrapf(err, "unable to open specified config file %q", configFile)
		}
		defer f.Close()
		sshOperator.CopySCP(f, rke2ConfigFile)
	}

	if registriesFile != "" {
		f, err := os.Open(registriesFile)
		if err != nil {
			return errors.Wrapf(err, "unable to open specified config file %q", registriesFile)
		}
		defer f.Close()
		sshOperator.CopySCP(f, containerdRegistriesFile)
	}

	installStr := createVersionStr(rke2Version, rke2Channel)
	installRKE2Exec := installStr + " sh -s -"

	rkeConfig := makeConfig(serverHost, strings.TrimSpace(joinToken))

	populateConfig := fmt.Sprintf("echo '%s' | %s tee -a "+rke2ConfigFile, rkeConfig, sudoPrefix)
	ensureSystemdcommand := fmt.Sprintf("%s systemctl enable --no-block --now rke2-agent", sudoPrefix)

	installAgentCommand := fmt.Sprintf("%s | %s %s", getScript, sudoPrefix, installRKE2Exec)

	if printCommand {
		fmt.Printf("ssh: %s\n", installAgentCommand)
	}

	_, err := sshOperator.Execute(populateConfig)
	if err != nil {
		return err
	}

	res, err := sshOperator.Execute(installAgentCommand)

	if err != nil {
		return errors.Wrap(err, "unable to setup agent")
	}

	fmt.Printf("🐌 Joining agent node to cluster, please be patient while services start...\n")
	_, err = sshOperator.Execute(ensureSystemdcommand)
	if err != nil {
		return err
	}

	if len(res.StdErr) > 0 {
		fmt.Printf("Logs: %s", res.StdErr)
	}

	joinRes := string(res.StdOut)
	fmt.Printf("Output: %s", string(joinRes))

	return nil
}

func createVersionStr(rke2Version, Channel string) string {
	installStr := ""
	if len(rke2Version) > 0 {
		installStr = fmt.Sprintf("INSTALL_RKE2_VERSION='%s'", rke2Version)
	} else {
		installStr = fmt.Sprintf("INSTALL_RKE2_CHANNEL='%s'", Channel)
	}
	return installStr
}

func makeConfig(server, token string) string {
	return fmt.Sprintf("server: https://%s:9345 \ntoken: %s\n", server, token)
}
