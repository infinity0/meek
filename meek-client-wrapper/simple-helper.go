// +build !windows

package main

const browserHelperToCmdLineHelp = "path to browser helper executable"

// Convert the helper filename into a command line string slice
func browserHelperToCmdLine(browserHelperPath string) (command []string, err error) {
	command = []string{browserHelperPath}
	return
}
