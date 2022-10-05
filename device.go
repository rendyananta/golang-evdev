//go:build linux

package evdev

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

// InputDevice A Linux input device from which events can be read.
type InputDevice struct {
	Fn string // path to input device (devnode)

	Name  string   // device name
	Phys  string   // physical topology of device
	Ident string   // unique identifier
	File  *os.File // an open file handle to the input device

	BusType uint16 // bus type identifier
	Vendor  uint16 // vendor identifier
	Product uint16 // product identifier
	Version uint16 // version identifier

	EvdevVersion int // evdev protocol version

	Capabilities     map[CapabilityType][]CapabilityCode // supported event types and codes.
	CapabilitiesFlat map[int][]int
}

// Open an evdev input device.
func Open(devnode string) (*InputDevice, error) {
	f, err := os.Open(devnode)
	if err != nil {
		return nil, err
	}

	dev := InputDevice{}
	dev.Fn = devnode
	dev.File = f

	err = dev.setDeviceInfo()
	if err != nil {
		return nil, err
	}
	err = dev.setDeviceCapabilities()
	if err != nil {
		return nil, err
	}

	return &dev, nil
}

// Read and return a slice of input events from device.
func (dev *InputDevice) Read() ([]InputEvent, error) {
	events := make([]InputEvent, 16)
	buffer := make([]byte, eventsize*16)

	_, err := dev.File.Read(buffer)
	if err != nil {
		return events, err
	}

	b := bytes.NewBuffer(buffer)
	err = binary.Read(b, binary.LittleEndian, &events)
	if err != nil {
		return events, err
	}

	// remove trailing structures
	for i := range events {
		if events[i].Time.Sec == 0 {
			events = append(events[:i])
			break
		}
	}

	return events, err
}

// ReadOne Read and return a single input event.
func (dev *InputDevice) ReadOne() (*InputEvent, error) {
	event := InputEvent{}
	buffer := make([]byte, eventsize)

	_, err := dev.File.Read(buffer)
	if err != nil {
		return &event, err
	}

	b := bytes.NewBuffer(buffer)
	err = binary.Read(b, binary.LittleEndian, &event)
	if err != nil {
		return &event, err
	}

	return &event, err
}

// Get a useful description for an input device. Example:
//
//	InputDevice /dev/input/event3 (fd 3)
//	  name Logitech USB Laser Mouse
//	  phys usb-0000:00:12.0-2/input0
//	  bus 0x3, vendor 0x46d, product 0xc069, version 0x110
//	  events EV_KEY 1, EV_SYN 0, EV_REL 2, EV_MSC 4
func (dev *InputDevice) String() string {
	evTypes := make([]string, 0)

	for ev := range dev.Capabilities {
		evTypes = append(evTypes, fmt.Sprintf("%s %d", ev.Name, ev.Type))
	}
	rawEvTypes := strings.Join(evTypes, ", ")

	return fmt.Sprintf(
		"InputDevice %s (fd %d)\n"+
			"  name %s\n"+
			"  phys %s\n"+
			"  ident %s\n"+
			"  bus 0x%04x, vendor 0x%04x, product 0x%04x, version 0x%04x\n"+
			"  events %s",
		dev.Fn, dev.File.Fd(), dev.Name, dev.Phys, dev.Ident, dev.BusType,
		dev.Vendor, dev.Product, dev.Version, rawEvTypes)
}

// Gets the event types and event codes that the input device supports.
func (dev *InputDevice) setDeviceCapabilities() error {
	// Capabilities is a map of supported event types to lists of
	// events e.g: {1: [272, 273, 274, 275], 2: [0, 1, 6, 8]}
	// capabilities := make(map[int][]int)
	capabilities := make(map[CapabilityType][]CapabilityCode)

	evBits := new([(EV_MAX + 1) / 8]byte)
	codeBits := new([(KEY_MAX + 1) / 8]byte)
	// absbits  := new([6]byte)

	err := ioctl(dev.File.Fd(), uintptr(EVIOCGBIT(0, EV_MAX)), unsafe.Pointer(evBits))
	if err != 0 {
		return err
	}

	// Build a map of the device's capabilities
	for evType := 0; evType < EV_MAX; evType++ {
		if evBits[evType/8]&(1<<uint(evType%8)) != 0 {
			eventCodes := make([]CapabilityCode, 0)

			err = ioctl(dev.File.Fd(), uintptr(EVIOCGBIT(evType, KEY_MAX)), unsafe.Pointer(codeBits))
			if err != 0 {
				// ignore invalid capabilities such as EV_REP for some devices
				if err == syscall.EINVAL {
					continue
				}

				return err
			}

			for evCode := 0; evCode < KEY_MAX; evCode++ {
				if codeBits[evCode/8]&(1<<uint(evCode%8)) != 0 {
					c := CapabilityCode{evCode, ByEventType[evType][evCode]}
					eventCodes = append(eventCodes, c)
				}
			}

			// capabilities[EV_KEY] = [KEY_A, KEY_B, KEY_C, ...]
			key := CapabilityType{evType, EV[evType]}
			capabilities[key] = eventCodes
		}
	}

	dev.Capabilities = capabilities
	return nil
}

// An all-in-one function for describing an input device.
func (dev *InputDevice) setDeviceInfo() error {
	info := deviceInfo{}

	name := new([MAX_NAME_SIZE]byte)
	phys := new([MAX_NAME_SIZE]byte)
	ident := new([MAX_NAME_SIZE]byte)

	err := ioctl(dev.File.Fd(), uintptr(EVIOCGID), unsafe.Pointer(&info))
	if err != 0 {
		return err
	}

	err = ioctl(dev.File.Fd(), uintptr(EVIOCGNAME), unsafe.Pointer(name))
	if err != 0 {
		return err
	}

	// it's ok if the topology info is not available
	ioctl(dev.File.Fd(), uintptr(EVIOCGPHYS), unsafe.Pointer(phys))

	// it's ok if the unique identifier is not available
	ioctl(dev.File.Fd(), uintptr(EVIOCGUNIQ), unsafe.Pointer(ident))

	dev.Name = bytesToString(name)
	dev.Phys = bytesToString(phys)
	dev.Ident = bytesToString(ident)

	dev.Vendor = info.vendor
	dev.BusType = info.busType
	dev.Product = info.product
	dev.Version = info.version

	evVersion := new(int)
	err = ioctl(dev.File.Fd(), uintptr(EVIOCGVERSION), unsafe.Pointer(evVersion))
	if err != 0 {
		return err
	}
	dev.EvdevVersion = *evVersion

	return nil
}

// GetRepeatRate as a two element array.
//
//	[0] repeat rate in characters per second
//	[1] amount of time that a key must be depressed before it will start
//	    to repeat (in milliseconds)
func (dev *InputDevice) GetRepeatRate() *[2]uint {
	repeatDelay := new([2]uint)
	ioctl(dev.File.Fd(), uintptr(EVIOCGREP), unsafe.Pointer(repeatDelay))

	return repeatDelay
}

// SetRepeatRate Set repeat rate and delay.
func (dev *InputDevice) SetRepeatRate(repeat, delay uint) {
	repeatDelay := new([2]uint)
	repeatDelay[0], repeatDelay[1] = repeat, delay
	ioctl(dev.File.Fd(), uintptr(EVIOCSREP), unsafe.Pointer(repeatDelay))
}

// Grab the input device exclusively.
func (dev *InputDevice) Grab() error {
	grab := int(1)
	if err := ioctl(dev.File.Fd(), uintptr(EVIOCGRAB), unsafe.Pointer(&grab)); err != 0 {
		return err
	}

	return nil
}

// Release a grabbed input device.
func (dev *InputDevice) Release() error {
	if err := ioctl(dev.File.Fd(), uintptr(EVIOCGRAB), unsafe.Pointer(nil)); err != 0 {
		return err
	}

	return nil
}

type CapabilityType struct {
	Type int
	Name string
}

type CapabilityCode struct {
	Code int
	Name string
}

type AbsInfo struct {
	value      int32
	minimum    int32
	maximum    int32
	fuzz       int32
	flat       int32
	resolution int32
}

// Corresponds to the input_id struct.
type deviceInfo struct {
	busType, vendor, product, version uint16
}

// Return the keys of a map as a slice (dict.keys())
func keys(cap *map[int][]int) []int {
	slice := make([]int, 0)

	for key := range *cap {
		slice = append(slice, key)
	}

	return slice
}

// IsInputDevice determine if a path exist and is a character input device.
func IsInputDevice(path string) bool {
	fi, err := os.Stat(path)

	if os.IsNotExist(err) {
		return false
	}

	m := fi.Mode()
	if m&os.ModeCharDevice == 0 {
		return false
	}

	return true
}

// ListInputDevicePaths return a list of accessible input device names matched by
// deviceglob (default '/dev/input/event*').
func ListInputDevicePaths(deviceGlob string) ([]string, error) {
	paths, err := filepath.Glob(deviceGlob)

	if err != nil {
		return nil, err
	}

	devices := make([]string, 0)
	for _, path := range paths {
		if IsInputDevice(path) {
			devices = append(devices, path)
		}
	}

	return devices, nil
}

// ListInputDevices Return a list of accessible input devices matched by deviceglob
// (default '/dev/input/event/*').
func ListInputDevices(deviceGlobArg ...string) ([]*InputDevice, error) {
	deviceGlob := "/dev/input/event*"
	if len(deviceGlobArg) > 0 {
		deviceGlob = deviceGlobArg[0]
	}

	fns, _ := ListInputDevicePaths(deviceGlob)
	devices := make([]*InputDevice, 0)

	for i := range fns {
		dev, err := Open(fns[i])
		if err == nil {
			devices = append(devices, dev)
		}
	}

	return devices, nil
}

func bytesToString(b *[MAX_NAME_SIZE]byte) string {
	idx := bytes.IndexByte(b[:], 0)
	return string(b[:idx])
}
