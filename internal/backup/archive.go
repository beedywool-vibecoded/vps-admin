// Package backup writes the panel state to an encrypted archive on Google Drive (through
// rclone) and restores it. The archive is self-contained: the database snapshot and the
// key that decrypts the values it holds travel together, protected by a passphrase the
// administrator chooses. Nothing readable by Google is ever uploaded.
package backup

import (
	"archive/tar"
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/scrypt"
)

// File layout: magic, salt, nonce prefix, then the payload encrypted in fixed chunks,
// each AEAD-sealed with nonce prefix || counter. A wrong passphrase fails on the first
// chunk, so it is reported as such instead of as a corrupt file.
const (
	magic          = "VPSADMIN-BACKUP\x01"
	saltLen        = 16
	noncePrefixLen = 8
	chunkSize      = 64 << 10
	scryptN        = 1 << 15 // ~32 MB of memory, ~100 ms: enough to make guessing costly
	scryptR        = 8
	scryptP        = 1
	keyLen         = 32

	// MaxArchive bounds what restore will read into a temporary folder. A database of a
	// small server stays far below; the volumes of the full backup will raise it.
	MaxArchive = 4 << 30

	manifestName = "manifest.json"
)

// Header describes an archive without decrypting the rest of it. It travels as the first
// entry, so a restore can check the kind and the server before touching anything.
type Header struct {
	Version   int       `json:"version"`
	Kind      string    `json:"kind"` // settings | full
	Created   time.Time `json:"created"`
	Panel     string    `json:"panel"` // version of the agent that wrote it
	Host      string    `json:"host"`
	PublicURL string    `json:"publicUrl,omitempty"`
}

// Entry is one file written into the payload. Data carries small values (secret.key,
// config); Path streams a file from disk (the database snapshot), which keeps a large
// backup out of memory.
type Entry struct {
	Name string
	Mode int64
	Data []byte
	Path string
}

var ErrBadPassphrase = errors.New("wrong passphrase, or the archive is damaged")

// ArchiveName is the file name of a backup: fixed prefix, kind and UTC timestamp, so the
// names sort in chronological order.
func ArchiveName(kind string, t time.Time) string {
	return fmt.Sprintf("vps-admin-%s-%s.vpb", kind, t.UTC().Format("20060102T150405"))
}

// Write seals entries into dest. It writes to a temporary file first: a backup in
// progress must never look like a complete archive on the remote.
func Write(dest, passphrase string, h Header, entries []Entry) (err error) {
	if len(passphrase) < minPassphrase {
		return fmt.Errorf("the passphrase must be at least %d characters", minPassphrase)
	}
	payload, err := os.CreateTemp(filepath.Dir(dest), ".payload-*.tar.gz")
	if err != nil {
		return err
	}
	defer os.Remove(payload.Name())
	defer payload.Close()
	if err := writePayload(payload, h, entries); err != nil {
		return err
	}
	if _, err := payload.Seek(0, io.SeekStart); err != nil {
		return err
	}

	out, err := os.CreateTemp(filepath.Dir(dest), ".archive-*.tmp")
	if err != nil {
		return err
	}
	defer func() {
		out.Close()
		if err != nil {
			os.Remove(out.Name())
		}
	}()

	salt := make([]byte, saltLen)
	prefix := make([]byte, noncePrefixLen)
	if _, err := rand.Read(salt); err != nil {
		return err
	}
	if _, err := rand.Read(prefix); err != nil {
		return err
	}
	if _, err := out.Write([]byte(magic)); err != nil {
		return err
	}
	if _, err := out.Write(salt); err != nil {
		return err
	}
	if _, err := out.Write(prefix); err != nil {
		return err
	}
	aead, err := newAEAD(passphrase, salt)
	if err != nil {
		return err
	}
	if err := seal(out, aead, prefix, payload); err != nil {
		return err
	}
	if err := out.Sync(); err != nil {
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Rename(out.Name(), dest)
}

func writePayload(w io.Writer, h Header, entries []Entry) error {
	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)
	h.Version = 1
	manifest, err := json.MarshalIndent(h, "", "  ")
	if err != nil {
		return err
	}
	if err := tw.WriteHeader(&tar.Header{Name: manifestName, Mode: 0o600, Size: int64(len(manifest)), ModTime: h.Created}); err != nil {
		return err
	}
	if _, err := tw.Write(manifest); err != nil {
		return err
	}
	for _, e := range entries {
		if err := writeEntry(tw, e); err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}

func writeEntry(tw *tar.Writer, e Entry) error {
	if !safeName(e.Name) {
		return fmt.Errorf("invalid entry name %q", e.Name)
	}
	if e.Path != "" {
		f, err := os.Open(e.Path)
		if err != nil {
			return err
		}
		defer f.Close()
		st, err := f.Stat()
		if err != nil {
			return err
		}
		if err := tw.WriteHeader(&tar.Header{Name: e.Name, Mode: 0o600, Size: st.Size(), ModTime: st.ModTime()}); err != nil {
			return err
		}
		_, err = io.Copy(tw, f)
		return err
	}
	if err := tw.WriteHeader(&tar.Header{Name: e.Name, Mode: e.Mode, Size: int64(len(e.Data)), ModTime: time.Now()}); err != nil {
		return err
	}
	_, err := tw.Write(e.Data)
	return err
}

// Extract decrypts path into dir and returns its header with the entry names it wrote.
// Entries keep their base names (and one level of folder for volumes), never absolute
// paths or ..
func Extract(path, passphrase, dir string) (Header, []string, error) {
	var h Header
	f, err := os.Open(path)
	if err != nil {
		return h, nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return h, nil, err
	}
	if st.Size() > MaxArchive {
		return h, nil, fmt.Errorf("the archive is larger than %d GB", MaxArchive>>30)
	}
	head := make([]byte, len(magic)+saltLen+noncePrefixLen)
	if _, err := io.ReadFull(f, head); err != nil {
		return h, nil, errors.New("not a vps-admin backup")
	}
	if string(head[:len(magic)]) != magic {
		return h, nil, errors.New("not a vps-admin backup")
	}
	salt := head[len(magic) : len(magic)+saltLen]
	prefix := head[len(magic)+saltLen:]
	aead, err := newAEAD(passphrase, salt)
	if err != nil {
		return h, nil, err
	}
	dec, err := os.CreateTemp(dir, ".payload-*.tar.gz")
	if err != nil {
		return h, nil, err
	}
	defer os.Remove(dec.Name())
	defer dec.Close()
	if err := open(dec, aead, prefix, f); err != nil {
		return h, nil, err
	}
	if _, err := dec.Seek(0, io.SeekStart); err != nil {
		return h, nil, err
	}
	gz, err := gzip.NewReader(dec)
	if err != nil {
		return h, nil, errors.New("the archive is damaged")
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	var names []string
	for {
		th, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return h, nil, errors.New("the archive is damaged")
		}
		if !safeName(th.Name) {
			return h, nil, fmt.Errorf("the archive holds an unsafe path: %q", th.Name)
		}
		if th.Name == manifestName {
			if err := json.NewDecoder(tr).Decode(&h); err != nil {
				return h, nil, errors.New("the archive manifest is unreadable")
			}
			if h.Version != 1 {
				return h, nil, fmt.Errorf("this backup comes from a newer panel (format %d)", h.Version)
			}
			continue
		}
		target := filepath.Join(dir, th.Name)
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return h, nil, err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			return h, nil, err
		}
		// CopyN bounds what a lying header could write.
		if _, err := io.CopyN(out, tr, th.Size); err != nil {
			out.Close()
			return h, nil, errors.New("the archive is truncated")
		}
		if err := out.Close(); err != nil {
			return h, nil, err
		}
		names = append(names, th.Name)
	}
	if h.Kind == "" {
		return h, nil, errors.New("the archive has no manifest")
	}
	return h, names, nil
}

func newAEAD(passphrase string, salt []byte) (cipher.AEAD, error) {
	key, err := scrypt.Key([]byte(passphrase), salt, scryptN, scryptR, scryptP, keyLen)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func nonce(prefix []byte, n uint32) []byte {
	b := make([]byte, len(prefix)+4)
	copy(b, prefix)
	binary.BigEndian.PutUint32(b[len(prefix):], n)
	return b
}

func seal(dst io.Writer, aead cipher.AEAD, prefix []byte, src io.Reader) error {
	buf := make([]byte, chunkSize)
	var n uint32
	for {
		read, err := io.ReadFull(src, buf)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
			return err
		}
		if read > 0 {
			ct := aead.Seal(nil, nonce(prefix, n), buf[:read], nil)
			if _, err := dst.Write(ct); err != nil {
				return err
			}
			n++
		}
		if errors.Is(err, io.ErrUnexpectedEOF) {
			return nil
		}
	}
}

func open(dst io.Writer, aead cipher.AEAD, prefix []byte, src io.Reader) error {
	buf := make([]byte, chunkSize+aead.Overhead())
	var n uint32
	for {
		read, err := io.ReadFull(src, buf)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
			return err
		}
		if read < aead.Overhead() {
			return ErrBadPassphrase
		}
		pt, err := aead.Open(nil, nonce(prefix, n), buf[:read], nil)
		if err != nil {
			// The first chunk failing means the key is wrong; a later one means the file
			// was cut or altered. Both are reported the same way on purpose.
			return ErrBadPassphrase
		}
		if _, err := dst.Write(pt); err != nil {
			return err
		}
		n++
		if errors.Is(err, io.ErrUnexpectedEOF) {
			return nil
		}
	}
}

// safeName accepts a plain file or one folder level (volumes/<app>/<file>), never an
// absolute path, a traversal or a hidden file.
func safeName(name string) bool {
	if name == "" || strings.HasPrefix(name, "/") || strings.Contains(name, "\\") || strings.Contains(name, "\x00") {
		return false
	}
	if strings.HasPrefix(name, ".") {
		return false
	}
	parts := strings.Split(name, "/")
	if len(parts) > 3 {
		return false
	}
	for _, p := range parts {
		if p == "" || p == "." || p == ".." || strings.HasPrefix(p, ".") {
			return false
		}
	}
	return true
}
