package deb

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"io/ioutil"
	"path/filepath"
	"strings"

	"github.com/midbel/packit"
	"github.com/midbel/packit/deb/control"
	"github.com/midbel/tape"
)

type pkg struct {
	name string

	control   *bytes.Reader
	md5sums   *bytes.Reader
	conffiles *bytes.Reader

	data *bytes.Reader
}

func (p *pkg) PackageName() string {
	return p.name
}

func (p *pkg) Valid() error {
	if _, err := p.md5sums.Seek(0, io.SeekStart); err != nil {
		return err
	}
	ds := make(map[string]string)
	s := bufio.NewScanner(p.md5sums)
	for s.Scan() {
		fs := strings.Fields(s.Text())
		ds[fs[1]] = fs[0]
	}
	if err := s.Err(); err != nil {
		return err
	}
	if _, err := p.data.Seek(0, io.SeekStart); err != nil {
		return err
	}
	r := tar.NewReader(p.data)
	for {
		h, err := r.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		digest := md5.New()
		if _, err := io.CopyN(digest, r, h.Size); err != nil {
			return err
		}
		sum := hex.EncodeToString(digest.Sum(nil))
		if s, ok := ds[h.Name]; ok {
			if s != sum {
				return fmt.Errorf("invalid checksum for %s", h.Name)
			}
		} else {
			return fmt.Errorf("file not found in md5sums %s", h.Name)
		}
	}
	return nil
}

func (p *pkg) About() packit.Control {
	var c packit.Control
	if _, err := p.control.Seek(0, io.SeekStart); err != nil {
		return c
	}
	if x, err := control.Parse(p.control); err == nil {
		c = *x
	}
	return c
}

func (p *pkg) Filenames() ([]string, error) {
	if _, err := p.data.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	r := tar.NewReader(p.data)
	var ns []string
	for {
		h, err := r.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		ns = append(ns, h.Name)
		if _, err := io.CopyN(ioutil.Discard, r, h.Size); err != nil {
			return nil, err
		}
	}
	return ns, nil
}

func readDebian(r tape.Reader) error {
	h, err := r.Next()
	if err != nil {
		return err
	}
	if h.Filename != debBinaryFile {
		return fmt.Errorf("malformed deb package: want %s, got %s", debBinaryFile, h.Filename)
	}
	bs := make([]byte, len(debVersion))
	if _, err := io.ReadFull(r, bs); err != nil {
		return err
	}
	if debVersion != string(bs) {
		return fmt.Errorf("unsupported deb version %s", bytes.TrimSpace(bs))
	}
	return nil
}

func readControl(r tape.Reader, p *pkg) error {
	h, err := r.Next()
	if err != nil {
		return err
	}
	if h.Filename != debControlTar {
		return fmt.Errorf("malformed deb package: want %s, got: %s", debControlTar, h.Filename)
	}
	z, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	t := tar.NewReader(z)
	for {
		h, err := t.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		bs := make([]byte, h.Size)
		if _, err := io.ReadFull(t, bs); err != nil {
			return err
		}
		switch h.Name {
		case debControlFile, "./" + debControlFile:
			p.control = bytes.NewReader(bs)
		case debSumFile, "./" + debSumFile:
			p.md5sums = bytes.NewReader(bs)
		case debConfFile, "./" + debConfFile:
			p.conffiles = bytes.NewReader(bs)
		default:
		}
	}
	return nil
}

func readData(r tape.Reader, p *pkg) error {
	h, err := r.Next()
	if err != nil {
		return err
	}
	var rs io.Reader
	switch e := filepath.Ext(h.Filename); e {
	case ".gz":
		z, err := gzip.NewReader(r)
		if err != nil {
			return err
		}
		rs = z
	case ".xz":
		return fmt.Errorf("not yet supported format %s", e)
	default:
		return fmt.Errorf("unsupported format %s", e)
	}
	bs, err := ioutil.ReadAll(rs)
	if err == nil {
		p.data = bytes.NewReader(bs)
	}
	return err
}