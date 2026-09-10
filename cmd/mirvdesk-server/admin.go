package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/term"
)

func runAdminCLIIfRequested() bool {
	args := os.Args[1:]
	name := filepath.Base(os.Args[0])
	if name != "mirvdesk-admin" {
		if len(args) == 0 || args[0] != "admin" {
			return false
		}
		args = args[1:]
	}

	if err := runAdminCLI(args); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
	return true
}
func runAdminCLI(args []string) error {
	dataDir := env("MIRVDESK_DATA_DIR", "/data")
	st, err := openStore(dataDir)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer st.close()

	if len(args) == 0 {
		if n, err := st.userCount(); err != nil {
			return err
		} else if n == 0 {
			fmt.Println("No users found. Create the first administrator.")
			return adminCreate(st)
		}
		return runAdminMenu(st)
	}
	switch args[0] {
	case "create-admin":
		return adminCreate(st)
	case "create-user":
		return adminCreateUser(st)
	case "passwd":
		username := ""
		if len(args) > 1 {
			username = args[1]
		}
		return adminPassword(st, username)
	case "list":
		return adminList(st)
	case "group-create":
		if len(args) < 2 {
			return fmt.Errorf("usage: mirvdesk-admin group-create <name>")
		}
		return st.createDeviceGroup(strings.Join(args[1:], " "))
	case "group-add-user":
		if len(args) != 3 {
			return fmt.Errorf("usage: mirvdesk-admin group-add-user <group> <login>")
		}
		return st.addUserToDeviceGroup(args[1], args[2])
	case "group-remove-user":
		if len(args) != 3 {
			return fmt.Errorf("usage: mirvdesk-admin group-remove-user <group> <login>")
		}
		return st.removeUserFromDeviceGroup(args[1], args[2])
	case "device-group":
		if len(args) != 3 {
			return fmt.Errorf("usage: mirvdesk-admin device-group <peer-id> <group|none>")
		}
		return st.setDeviceGroup(args[1], args[2])
	case "groups":
		return adminListGroups(st)
	case "devices":
		return adminListDevices(st)
	case "help", "-h", "--help":
		printAdminUsage()
		return nil
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}
func runAdminMenu(st *store) error {
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Println("MirvDesk administration")
		fmt.Println("1) Create administrator")
		fmt.Println("2) Change user password")
		fmt.Println("3) List users")
		fmt.Println("4) Create regular user")
		fmt.Println("5) List device groups")
		fmt.Println("6) List registered devices")
		fmt.Println("0) Exit")
		choice, err := promptLine(reader, "Select: ")
		if err != nil {
			return err
		}
		fmt.Println()
		switch choice {
		case "1":
			if err := adminCreate(st); err != nil {
				fmt.Fprintln(os.Stderr, "Error:", err)
			}
		case "2":
			if err := adminPassword(st, ""); err != nil {
				fmt.Fprintln(os.Stderr, "Error:", err)
			}
		case "3":
			if err := adminList(st); err != nil {
				fmt.Fprintln(os.Stderr, "Error:", err)
			}
		case "4":
			if err := adminCreateUser(st); err != nil {
				fmt.Fprintln(os.Stderr, "Error:", err)
			}
		case "5":
			if err := adminListGroups(st); err != nil {
				fmt.Fprintln(os.Stderr, "Error:", err)
			}
		case "6":
			if err := adminListDevices(st); err != nil {
				fmt.Fprintln(os.Stderr, "Error:", err)
			}
		case "0", "q", "quit", "exit":
			return nil
		default:
			fmt.Println("Unknown selection")
		}
		fmt.Println()
	}
}
func adminCreate(st *store) error {
	reader := bufio.NewReader(os.Stdin)
	username, err := promptLine(reader, "Login: ")
	if err != nil {
		return err
	}
	displayName, err := promptLine(reader, "Display name (optional): ")
	if err != nil {
		return err
	}
	password, err := promptPasswordTwice()
	if err != nil {
		return err
	}
	u, err := st.createAdmin(username, password, displayName)
	if err != nil {
		return err
	}
	fmt.Printf("Administrator %q created.\n", u.Name)
	return nil
}

func adminCreateUser(st *store) error {
	reader := bufio.NewReader(os.Stdin)
	username, err := promptLine(reader, "Login: ")
	if err != nil {
		return err
	}
	displayName, err := promptLine(reader, "Display name (optional): ")
	if err != nil {
		return err
	}
	password, err := promptPasswordTwice()
	if err != nil {
		return err
	}
	u, err := st.createUser(username, password, displayName, false)
	if err != nil {
		return err
	}
	fmt.Printf("User %q created.\n", u.Name)
	return nil
}

func adminPassword(st *store, username string) error {
	if strings.TrimSpace(username) == "" {
		reader := bufio.NewReader(os.Stdin)
		var err error
		username, err = promptLine(reader, "Login: ")
		if err != nil {
			return err
		}
	}
	password, err := promptPasswordTwice()
	if err != nil {
		return err
	}
	if err := st.setPassword(username, password); err != nil {
		return err
	}
	fmt.Printf("Password for %q changed; existing sessions revoked.\n", strings.TrimSpace(username))
	return nil
}
func adminList(st *store) error {
	users, err := st.listUsers()
	if err != nil {
		return err
	}
	if len(users) == 0 {
		fmt.Println("No users.")
		return nil
	}
	fmt.Printf("%-24s %-8s %-8s %s\n", "LOGIN", "ADMIN", "STATUS", "DISPLAY NAME")
	for _, u := range users {
		admin := "no"
		if u.IsAdmin {
			admin = "yes"
		}
		status := "disabled"
		if u.Status == 1 {
			status = "active"
		}
		fmt.Printf("%-24s %-8s %-8s %s\n", u.Name, admin, status, u.DisplayName)
	}
	return nil
}

func adminListGroups(st *store) error {
	groups, err := st.listDeviceGroups()
	if err != nil {
		return err
	}
	if len(groups) == 0 {
		fmt.Println("No device groups.")
		return nil
	}
	fmt.Printf("%-28s %s\n", "GROUP", "NOTE")
	for _, g := range groups {
		fmt.Printf("%-28s %s\n", g.Name, g.Note)
	}
	return nil
}

func adminListDevices(st *store) error {
	devices, err := st.listDevices()
	if err != nil {
		return err
	}
	if len(devices) == 0 {
		fmt.Println("No registered devices. Devices appear after a user logs in from them.")
		return nil
	}
	fmt.Printf("%-16s %-24s %-24s %s\n", "PEER ID", "OWNER", "GROUP", "DEVICE")
	for _, d := range devices {
		name, _ := d.Info["device_name"].(string)
		fmt.Printf("%-16s %-24s %-24s %s\n", d.ID, d.UserName, d.DeviceGroupName, name)
	}
	return nil
}

func promptLine(reader *bufio.Reader, prompt string) (string, error) {
	fmt.Print(prompt)
	value, err := reader.ReadString('\n')
	if err != nil && len(value) == 0 {
		return "", err
	}
	return strings.TrimSpace(value), nil
}
func promptPasswordTwice() (string, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", fmt.Errorf("password input requires a TTY; use docker exec -it")
	}
	fmt.Print("Password: ")
	first, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		return "", err
	}
	fmt.Print("Repeat password: ")
	second, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		return "", err
	}
	if string(first) != string(second) {
		return "", fmt.Errorf("passwords do not match")
	}
	if len(first) < 10 {
		return "", fmt.Errorf("password must be at least 10 characters")
	}
	return string(first), nil
}

func printAdminUsage() {
	fmt.Println("Usage:")
	fmt.Println("  mirvdesk-admin                 interactive menu")
	fmt.Println("  mirvdesk-admin create-admin                 create an administrator")
	fmt.Println("  mirvdesk-admin create-user                  create a regular user")
	fmt.Println("  mirvdesk-admin passwd [login]               change a user's password")
	fmt.Println("  mirvdesk-admin list                         list users")
	fmt.Println("  mirvdesk-admin groups                       list device groups")
	fmt.Println("  mirvdesk-admin devices                      list registered devices")
	fmt.Println("  mirvdesk-admin group-create <name>          create device group")
	fmt.Println("  mirvdesk-admin group-add-user <group> <u>   grant user access to group")
	fmt.Println("  mirvdesk-admin group-remove-user <group> <u> revoke group access")
	fmt.Println("  mirvdesk-admin device-group <id> <group>    assign device (use none to clear)")
}
