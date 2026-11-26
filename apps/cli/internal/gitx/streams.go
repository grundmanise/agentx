package gitx

import (
	"errors"
	"io"
	"os"
)

// streams are the standard input, output and error of one git: files with
// no name, so that nothing is left behind when agentx dies.
type streams struct {
	in, out, err *os.File
}

// openStreams opens the files of one git, the input holding all of stdin.
// No stdin is the null device, which exec opens for a nil Stdin.
func openStreams(stdin io.Reader) (*streams, error) {
	s := &streams{}
	var err error
	if s.out, err = unnamedFile(); err == nil {
		s.err, err = unnamedFile()
	}
	if err == nil && stdin != nil {
		if s.in, err = unnamedFile(); err == nil {
			if _, err = io.Copy(s.in, stdin); err == nil {
				_, err = s.in.Seek(0, io.SeekStart)
			}
		}
	}
	if err != nil {
		s.close()
		return nil, err
	}
	return s, nil
}

// read gives what git wrote on stdout and on stderr.
func (s *streams) read() (string, string, error) {
	out, errOut := readAll(s.out)
	stderr, errErr := readAll(s.err)
	return out, stderr, errors.Join(errOut, errErr)
}

func (s *streams) close() {
	for _, f := range []*os.File{s.in, s.out, s.err} {
		if f != nil {
			f.Close()
		}
	}
}

func readAll(f *os.File) (string, error) {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	b, err := io.ReadAll(f)
	return string(b), err
}

// unnamedFile creates a temporary file and removes its name at once.
func unnamedFile() (*os.File, error) {
	f, err := os.CreateTemp("", "agentx-git-")
	if err != nil {
		return nil, err
	}
	if err := os.Remove(f.Name()); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}
