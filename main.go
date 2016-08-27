package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"
)

func main() {
	err := run()
	if err != nil {
		panic(err)
	}
}

func run() error {
	cmd := exec.Command("scp", "-tpr", "/tmp")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}

	err = cmd.Start()
	if err != nil {
		return err
	}

	err = func() error {
		defer stdin.Close()

		s, err := newSource(stdin, stdout)
		if err != nil {
			return err
		}

		err = s.setTime(
			time.Date(2006, 1, 2, 15, 04, 05, 678901000, time.Local),
			time.Date(2018, 8, 31, 23, 59, 58, 999999000, time.Local),
		)
		if err != nil {
			return err
		}

		mode := os.FileMode(0644)
		filename := "test1"
		content := "content1\n"
		err = s.writeFile(mode, int64(len(content)), filename, bytes.NewBufferString(content))
		if err != nil {
			return err
		}

		err = s.startDirectory(os.FileMode(0755), "test2")
		if err != nil {
			return err
		}

		err = s.startDirectory(os.FileMode(0750), "sub")
		if err != nil {
			return err
		}

		mode = os.FileMode(0604)
		filename = "test2"
		content = ""
		err = s.writeFile(mode, int64(len(content)), filename, bytes.NewBufferString(content))
		if err != nil {
			return err
		}

		err = s.endDirectory()
		if err != nil {
			return err
		}

		err = s.endDirectory()
		if err != nil {
			return err
		}

		return nil
	}()
	if err != nil {
		return err
	}

	err = cmd.Wait()
	if err != nil {
		return err
	}
	return nil
}

const (
	msgCopyFile       = 'C'
	msgStartDirectory = 'D'
	msgEndDirectory   = 'E'
	msgTime           = 'T'
)

const (
	replyOK         = '\x00'
	replyError      = '\x01'
	replyFatalError = '\x02'
)

type source struct {
	remIn     io.WriteCloser
	remOut    io.Reader
	remReader *bufio.Reader
}

func newSource(remIn io.WriteCloser, remOut io.Reader) (*source, error) {
	s := &source{
		remIn:     remIn,
		remOut:    remOut,
		remReader: bufio.NewReader(remOut),
	}

	return s, s.readReply()
}

func (s *source) setTime(mtime, atime time.Time) error {
	ms, mus := secondsAndMicroseconds(mtime)
	as, aus := secondsAndMicroseconds(atime)
	_, err := fmt.Fprintf(s.remIn, "%c%d %d %d %d\n", msgTime, ms, mus, as, aus)
	if err != nil {
		return fmt.Errorf("failed to write scp time header: err=%s", err)
	}
	return s.readReply()
}

func secondsAndMicroseconds(t time.Time) (seconds int64, microseconds int) {
	rounded := t.Round(time.Microsecond)
	return rounded.Unix(), rounded.Nanosecond() / int(int64(time.Microsecond)/int64(time.Nanosecond))
}

func (s *source) writeFile(mode os.FileMode, length int64, filename string, body io.Reader) error {
	_, err := fmt.Fprintf(s.remIn, "%c%#4o %d %s\n", msgCopyFile, mode, length, filename)
	if err != nil {
		return fmt.Errorf("failed to write scp file header: err=%s", err)
	}
	_, err = io.Copy(s.remIn, body)
	if err != nil {
		return fmt.Errorf("failed to write scp file body: err=%s", err)
	}
	err = s.readReply()
	if err != nil {
		return err
	}

	_, err = s.remIn.Write([]byte{replyOK})
	if err != nil {
		return fmt.Errorf("failed to write scp replyOK reply: err=%s", err)
	}
	return s.readReply()
}

func (s *source) startDirectory(mode os.FileMode, dirname string) error {
	// length is not used.
	length := 0
	_, err := fmt.Fprintf(s.remIn, "%c%#4o %d %s\n", msgStartDirectory, mode, length, dirname)
	if err != nil {
		return fmt.Errorf("failed to write scp start directory header: err=%s", err)
	}
	return s.readReply()
}

func (s *source) endDirectory() error {
	_, err := fmt.Fprintf(s.remIn, "%c\n", msgEndDirectory)
	if err != nil {
		return fmt.Errorf("failed to write scp end directory header: err=%s", err)
	}
	return s.readReply()
}

type SCPError struct {
	msg   string
	fatal bool
}

func (e *SCPError) Error() string { return e.msg }
func (e *SCPError) Fatal() bool   { return e.fatal }

func (s *source) readReply() error {
	b, err := s.remReader.ReadByte()
	if err != nil {
		return fmt.Errorf("failed to read scp reply type: err=%s", err)
	}
	if b == replyOK {
		return nil
	}
	if b != replyError && b != replyFatalError {
		return fmt.Errorf("unexpected scp reply type: %v", b)
	}
	var line []byte
	line, err = s.remReader.ReadBytes('\n')
	if err != nil {
		return fmt.Errorf("failed to read scp reply message: err=%s", err)
	}
	return &SCPError{
		msg:   string(line),
		fatal: b == replyFatalError,
	}
}