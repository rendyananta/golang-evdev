//go:build linux

// Input device event monitor.
package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	evdev "github.com/rendyananta/golang-evdev"
)

const (
	usage      = "usage: evtest <device> [<type> <value>]"
	deviceGlob = "/dev/input/event*"
)

// selectDevice Select a device from a list of accessible input devices.
func selectDevice() (*evdev.InputDevice, error) {
	devices, _ := evdev.ListInputDevices(deviceGlob)

	lines := make([]string, 0)
	max := 0
	if len(devices) > 0 {
		for i := range devices {
			dev := devices[i]
			str := fmt.Sprintf("%-3d %-20s %-35s %-35s %s", i, dev.Fn, dev.Name, dev.Phys, dev.Ident)
			if len(str) > max {
				max = len(str)
			}
			lines = append(lines, str)
		}
		fmt.Printf("%-3s %-20s %-35s %-35s %s\n", "ID", "Device", "Name", "Phys", "Ident")
		fmt.Printf(strings.Repeat("-", max) + "\n")
		fmt.Printf(strings.Join(lines, "\n") + "\n")

		var choice int
		choiceMax := len(lines) - 1

	ReadChoice:
		fmt.Printf("Select device [0-%d]: ", choiceMax)
		_, err := fmt.Scan(&choice)
		if err != nil || choice > choiceMax || choice < 0 {
			goto ReadChoice
		}

		return devices[choice], nil
	}

	errmsg := fmt.Sprintf("no accessible input devices found by %s", deviceGlob)
	return nil, errors.New(errmsg)
}

func formatEvent(ev *evdev.InputEvent) string {
	var res, f, codeName string

	code := int(ev.Code)
	etype := int(ev.Type)

	switch ev.Type {
	case evdev.EV_SYN:
		if ev.Code == evdev.SYN_MT_REPORT {
			f = "time %d.%-8d +++++++++ %s ++++++++"
		} else {
			f = "time %d.%-8d --------- %s --------"
		}
		return fmt.Sprintf(f, ev.Time.Sec, ev.Time.Usec, evdev.SYN[code])
	case evdev.EV_KEY:
		val, haskey := evdev.KEY[code]
		if haskey {
			codeName = val
		} else {
			val, haskey := evdev.BTN[code]
			if haskey {
				codeName = val
			} else {
				codeName = "?"
			}
		}
	default:
		m, haskey := evdev.ByEventType[etype]
		if haskey {
			codeName = m[code]
		} else {
			codeName = "?"
		}
	}

	evfmt := "time %d.%-8d type %d (%s), code %-3d (%s), value %d"
	res = fmt.Sprintf(evfmt, ev.Time.Sec, ev.Time.Usec, etype,
		evdev.EV[int(ev.Type)], ev.Code, codeName, ev.Value)

	return res
}

func main() {
	var dev *evdev.InputDevice
	var events []evdev.InputEvent
	var err error

	switch len(os.Args) {
	case 1:
		dev, err = selectDevice()
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
	case 2:
		dev, err = evdev.Open(os.Args[1])
		if err != nil {
			fmt.Printf("unable to open input device: %s\n", os.Args[1])
			os.Exit(1)
		}
	default:
		fmt.Printf(usage + "\n")
		os.Exit(1)
	}

	info := fmt.Sprintf("bus 0x%04x, vendor 0x%04x, product 0x%04x, version 0x%04x",
		dev.BusType, dev.Vendor, dev.Product, dev.Version)

	repeatInfo := dev.GetRepeatRate()

	fmt.Printf("Evdev protocol version: %d\n", dev.EvdevVersion)
	fmt.Printf("Device name: %s\n", dev.Name)
	fmt.Printf("Device info: %s\n", info)
	fmt.Printf("Repeat settings: repeat %d. delay %d\n", repeatInfo[0], repeatInfo[1])
	fmt.Printf("Device capabilities:\n")

	fmt.Printf("Listening for events ...\n")

	for {
		events, err = dev.Read()
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		for i := range events {
			str := formatEvent(&events[i])
			fmt.Println(str)
		}
	}
}
