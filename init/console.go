package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

func consoleSetFont(c *VirtualConsole) error {
	if c.FontFile == "" {
		debug("setfont parameters are not specified")
		return nil
	}

	debug("loading font file %s", c.FontFile)
	args := []string{c.FontFile}
	if c.FontMapFile != "" {
		args = append(args, "-m", c.FontMapFile)
	}
	if c.FontUnicodeFile != "" {
		args = append(args, "-u", c.FontUnicodeFile)
	}
	cmd := exec.Command("setfont", args...)
	if debugEnabled {
		cmd.Stderr = os.Stderr
		cmd.Stdout = os.Stdout
	}
	return cmd.Run()
}

func loadKmap(fd uintptr, file string) error {
	blob, err := os.ReadFile(file)
	if err != nil {
		return err
	}

	curr := 0 // position of characters read from blob
	if !bytes.HasPrefix(blob, []byte("bkeymap")) {
		return fmt.Errorf("%s: is not a valid binary keymap", file)
	}
	curr = 7

	const (
		// from linux/kd.h
		KDGKBENT = 0x4B46 /* gets one entry in translation table */
		KDSKBENT = 0x4B47 /* sets one entry in translation table */

		NR_KEYS        = 128
		MAX_NR_KEYMAPS = 256
	)

	// load kmap
	keymaps := blob[curr : curr+MAX_NR_KEYMAPS]
	curr += MAX_NR_KEYMAPS

	for i, enabled := range keymaps {
		if enabled != 1 {
			continue
		}

		type kbentry struct {
			kb_table uint8
			kb_index uint8
			kb_value uint16
		}
		for j := 0; j < NR_KEYS; j++ {
			var ke kbentry

			ke.kb_table = uint8(i)
			ke.kb_index = uint8(j)
			ke.kb_value = *(*uint16)(unsafe.Pointer(&blob[curr]))
			curr += 2

			if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, KDSKBENT, uintptr(unsafe.Pointer(&ke))); errno != 0 {
				return os.NewSyscallError(fmt.Sprintf("ioctl (cmd=0x%x)", KDSKBENT), errno)
			}
		}
	}

	return nil
}

func consoleLoadKeymap(c *VirtualConsole) error {
	if c.KeymapFile == "" {
		debug("loadkey keymap is not specified")
		return nil
	}
	isUtf := c.Utf

	debug("loading keymap file %s", c.KeymapFile)

	cons, err := os.OpenFile("/dev/tty0", os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer cons.Close()

	const (
		// from linux/kd.h
		KDSETMODE = 0x4B3A
		KDGETMODE = 0x4B3B

		K_RAW       = 0x00
		K_XLATE     = 0x01
		K_MEDIUMRAW = 0x02
		K_UNICODE   = 0x03
	)

	var mode int
	var ctrl string
	if isUtf {
		mode = K_UNICODE
		ctrl = "\033%%G"
		// stty -F ${dev} iutf8
	} else {
		mode = K_XLATE
		ctrl = "\033%%@"
		// stty -F ${dev} -iutf8
	}
	// kbd_mode
	if err := unix.IoctlSetInt(int(cons.Fd()), KDSETMODE, mode); err != nil {
		return err
	}
	if _, err := cons.WriteString(ctrl); err != nil {
		return err
	}

	return loadKmap(cons.Fd(), c.KeymapFile)
}

func configureVirtualConsole() error {
	if c := config.VirtualConsole; c != nil {
		if err := consoleSetFont(c); err != nil {
			return err
		}
		if err := consoleLoadKeymap(c); err != nil {
			return err
		}
	}
	return nil
}
