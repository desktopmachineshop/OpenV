//go:build windows

package main

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows/registry"
)

// registerProtocol registers openv-connector:// for the current user (HKCU —
// no admin rights needed), pointing at this executable.
func registerProtocol() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}

	root, _, err := registry.CreateKey(registry.CURRENT_USER, `Software\Classes\openv-connector`, registry.ALL_ACCESS)
	if err != nil {
		return err
	}
	defer root.Close()
	if err := root.SetStringValue("", "URL:OpenV Agent Connector"); err != nil {
		return err
	}
	if err := root.SetStringValue("URL Protocol", ""); err != nil {
		return err
	}

	icon, _, err := registry.CreateKey(registry.CURRENT_USER, `Software\Classes\openv-connector\DefaultIcon`, registry.ALL_ACCESS)
	if err != nil {
		return err
	}
	defer icon.Close()
	if err := icon.SetStringValue("", exe+",0"); err != nil {
		return err
	}

	cmd, _, err := registry.CreateKey(registry.CURRENT_USER, `Software\Classes\openv-connector\shell\open\command`, registry.ALL_ACCESS)
	if err != nil {
		return err
	}
	defer cmd.Close()
	return cmd.SetStringValue("", fmt.Sprintf(`"%s" "%%1"`, exe))
}

func unregisterProtocol() error {
	for _, key := range []string{
		`Software\Classes\openv-connector\shell\open\command`,
		`Software\Classes\openv-connector\shell\open`,
		`Software\Classes\openv-connector\shell`,
		`Software\Classes\openv-connector\DefaultIcon`,
		`Software\Classes\openv-connector`,
	} {
		if err := registry.DeleteKey(registry.CURRENT_USER, key); err != nil && err != registry.ErrNotExist {
			return err
		}
	}
	return nil
}
