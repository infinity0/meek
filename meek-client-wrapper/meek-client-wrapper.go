// meek-client-wrapper is an auxiliary program that helps with connecting
// meek-client to meek-http-helper running in Tor Browser.
//
// Sample usage in torrc (exact paths depend on platform):
// 	ClientTransportPlugin meek exec ./meek-client-wrapper --log meek-client-wrapper.log --helper ./tbb-windows.bat -- ./meek-client --url=https://meek-reflect.appspot.com/ --front=www.google.com --log meek-client.log
// Everything up to "--" is options for this program. Everything following it is
// a meek-client command line.
//
// This program, meek-client-wrapper, starts a browser-helper program specified
// by the --helper option. This is executed with no arguments; use a shell
// script if you need something more complex. Alternatively, you may point to a
// file with the ".meek-browser-helper" suffix, which is a basic custom format
// for specifying what to execute. An example of its format:
//
// -- begin example file --
// # comments and empty lines are ignored
// OPTIONAL_SET_ENV1=value1
// OPTIONAL_UNSET_ENV=
// program_executable
// optional_arg1
// optional_arg2
// -- end example file --
//
// In any case, the browser-helper program should launch a browser process that
// has been configured to use the meek-http-helper extension, ideally in a
// separate browser profile not used for any other purpose.
//
// This program then reads the stdout of the helper, looking for a special
// line with the listening port number of the extension, one that looks like
// "meek-http-helper: listen <address>". The meek-client command is then
// executed as given, except that a --helper option is added to it, that points
// to the port number read from the browser-helper.
//
// This program proxies stdin and stdout to and from meek-client, so it is
// actually meek-client that drives the pluggable transport negotiation with
// tor.
package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
)

// This magic string is emitted by meek-http-helper.
var helperAddrPattern = regexp.MustCompile(`^meek-http-helper: listen (127\.0\.0\.1:\d+)$`)

func usage() {
	fmt.Fprintf(os.Stderr, "Usage: %s [meek-client-wrapper args] -- meek-client [meek-client args]\n", os.Args[0])
	flag.PrintDefaults()
}

// Log a call to os.Process.Kill.
func logKill(p *os.Process) error {
	log.Printf("killing PID %d", p.Pid)
	err := p.Kill()
	if err != nil {
		log.Print(err)
	}
	return err
}

// Log a call to os.Process.Signal.
func logSignal(p *os.Process, sig os.Signal) error {
	log.Printf("sending signal %s to PID %d", sig, p.Pid)
	err := p.Signal(sig)
	if err != nil {
		log.Print(err)
	}
	return err
}

// Convert the helper filename into a command line string slice
func browserHelperToCmdLine(browserHelperPath string) (command []string, err error) {
	if (strings.HasSuffix(browserHelperPath, ".meek-browser-helper")) {
		var file *os.File
		file, err = os.Open(browserHelperPath)
		if err != nil {
			return
		}
		defer file.Close()
		scanner := bufio.NewScanner(file)
		settingEnv := true
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			} else if settingEnv && strings.Contains(line, "=") {
				envpair := strings.SplitN(line, "=", 2)
				if envpair[1] == "" {
					log.Printf("unset envvar %s", envpair[0])
					err = os.Unsetenv(envpair[0])
				} else {
					log.Printf("set envvar %s=%s", envpair[0], envpair[1])
					err = os.Setenv(envpair[0], envpair[1])
				}
				if err != nil {
					return
				}
			} else {
				settingEnv = false
				command = append(command, line)
			}
		}
		err = scanner.Err()
		if err != nil {
			return
		}
		if (len(command) == 0) {
			err = errors.New("no commands in meek-browser-helper file: " + browserHelperPath)
			return
		}
	} else {
		command = []string{browserHelperPath}
	}
	return
}

// Run browser helper and return its exec.Cmd and stdout pipe.
func runBrowserHelper(browserHelperPath string) (cmd *exec.Cmd, stdout io.Reader, err error) {
	var command []string
	command, err = browserHelperToCmdLine(browserHelperPath)
	if err != nil {
		return
	}
	cmd = exec.Command(command[0], command[1:]...)
	cmd.Stderr = os.Stderr
	stdout, err = cmd.StdoutPipe()
	if err != nil {
		return
	}
	log.Printf("running browser-helper command %q", cmd.Args)
	err = cmd.Start()
	if err != nil {
		return
	}
	log.Printf("browser-helper started with pid %d", cmd.Process.Pid)
	return cmd, stdout, nil
}

// Look for the magic meek-http-helper address string in the Reader, and return
// the address it contains. Start a goroutine to continue reading and discarding
// output of the Reader before returning.
func grepHelperAddr(r io.Reader) (string, error) {
	var helperAddr string
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		if m := helperAddrPattern.FindStringSubmatch(line); m != nil {
			helperAddr = m[1]
			break
		}
	}
	err := scanner.Err()
	if err != nil {
		return "", err
	}
	// Ran out of input before finding the pattern.
	if helperAddr == "" {
		return "", io.EOF
	}
	// Keep reading from the browser to avoid its output buffer filling.
	go io.Copy(ioutil.Discard, r)
	return helperAddr, nil
}

// Run meek-client and return its exec.Cmd.
func runMeekClient(helperAddr string, meekClientCommandLine []string) (cmd *exec.Cmd, err error) {
	meekClientPath := meekClientCommandLine[0]
	args := meekClientCommandLine[1:]
	args = append(args, []string{"--helper", helperAddr}...)
	cmd = exec.Command(meekClientPath, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	log.Printf("running meek-client command %q", cmd.Args)
	err = cmd.Start()
	if err != nil {
		return
	}
	log.Printf("meek-client started with pid %d", cmd.Process.Pid)
	return cmd, nil
}

func main() {
	var logFilename string
	var browserHelperPath string

	flag.Usage = usage
	flag.StringVar(&logFilename, "log", "", "name of log file")
	flag.StringVar(&browserHelperPath, "helper", "", "path to browser helper executable")
	flag.Parse()

	if logFilename != "" {
		f, err := os.OpenFile(logFilename, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			log.Fatal(err)
		}
		defer f.Close()
		log.SetOutput(f)
	}

	if browserHelperPath == "" {
		log.Fatal("either specify a --helper, or run meek-client directly.")
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start browser-helper.
	browserHelperCmd, stdout, err := runBrowserHelper(browserHelperPath)
	if err != nil {
		log.Print(err)
		return
	}
	defer logKill(browserHelperCmd.Process)

	// Find out the helper's listening address.
	helperAddr, err := grepHelperAddr(stdout)
	if err != nil {
		log.Print(err)
		return
	}

	// Start meek-client with the helper address.
	meekClientCmd, err := runMeekClient(helperAddr, flag.Args())
	if err != nil {
		log.Print(err)
		return
	}
	defer logKill(meekClientCmd.Process)

	if os.Getenv("TOR_PT_EXIT_ON_STDIN_CLOSE") == "1" {
		// This environment variable means we should treat EOF on stdin
		// just like SIGTERM.
		// https://trac.torproject.org/projects/tor/ticket/15435
		go func() {
			io.Copy(ioutil.Discard, os.Stdin)
			log.Printf("synthesizing SIGTERM because of stdin close")
			sigChan <- syscall.SIGTERM
		}()
	}

	sig := <-sigChan
	log.Printf("sig %s", sig)
	err = logSignal(meekClientCmd.Process, sig)
	if err != nil {
		log.Print(err)
	}

	// If SIGINT, wait for a second SIGINT.
	if sig == syscall.SIGINT {
		sig := <-sigChan
		log.Printf("sig %s", sig)
		err = logSignal(meekClientCmd.Process, sig)
		if err != nil {
			log.Print(err)
		}
	}
}
