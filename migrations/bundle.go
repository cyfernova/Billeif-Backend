package migrationbundle

import (
	"bufio"
	"crypto/sha256"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const (
	IdentityFilename = "identities.txt"
	ManifestFilename = "manifest.sha256"
)

// Embedded is the canonical repository-root migration bundle.
//
//go:embed *.up.sql *.down.sql identities.txt manifest.sha256
var Embedded embed.FS

var (
	ErrChecksumMismatch = errors.New("migration checksum mismatch")
	ErrDuplicateEntry   = errors.New("duplicate migration entry")
	ErrMissingEntry     = errors.New("missing migration entry")
	ErrRenamedEntry     = errors.New("renamed migration entry")
)

var migrationFilename = regexp.MustCompile(`^([0-9]{6})_([a-z0-9]+(?:_[a-z0-9]+)*)\.(up|down)\.sql$`)
var migrationIdentity = regexp.MustCompile(`^([0-9]{6})_([a-z0-9]+(?:_[a-z0-9]+)*)$`)

type Entry struct {
	Name      string
	Version   uint
	Direction string
	SHA256    string
}

type Manifest struct {
	Entries       []Entry
	Digest        string
	LatestVersion uint
}

func Verify(fsys fs.FS, root string) (Manifest, error) {
	manifestPath := path.Join(root, ManifestFilename)
	body, err := fs.ReadFile(fsys, manifestPath)
	if err != nil {
		return Manifest{}, fmt.Errorf("%w: %s", ErrMissingEntry, ManifestFilename)
	}

	entries, err := parseManifest(body)
	if err != nil {
		return Manifest{}, err
	}
	if err := verifyFiles(fsys, root, entries); err != nil {
		return Manifest{}, err
	}
	if err := verifyIdentities(fsys, root, entries); err != nil {
		return Manifest{}, err
	}
	if err := verifyPairs(entries); err != nil {
		return Manifest{}, err
	}

	digest := sha256.Sum256(body)
	return Manifest{
		Entries:       entries,
		Digest:        fmt.Sprintf("%x", digest),
		LatestVersion: entries[len(entries)-1].Version,
	}, nil
}

func parseManifest(body []byte) ([]Entry, error) {
	if len(body) == 0 || body[len(body)-1] != '\n' {
		return nil, fmt.Errorf("%w: manifest must end with a newline", ErrChecksumMismatch)
	}

	var entries []Entry
	seenNames := make(map[string]struct{})
	seenDirections := make(map[string]struct{})
	previousName := ""
	scanner := bufio.NewScanner(strings.NewReader(string(body)))
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, "  ", 2)
		if len(parts) != 2 || len(parts[0]) != sha256.Size*2 {
			return nil, fmt.Errorf("%w: malformed manifest line", ErrChecksumMismatch)
		}
		checksum, name := parts[0], parts[1]
		if !isLowerHex(checksum) {
			return nil, fmt.Errorf("%w: malformed checksum", ErrChecksumMismatch)
		}
		matches := migrationFilename.FindStringSubmatch(name)
		if matches == nil {
			return nil, fmt.Errorf("%w: %s", ErrRenamedEntry, name)
		}
		if _, ok := seenNames[name]; ok {
			return nil, fmt.Errorf("%w: %s", ErrDuplicateEntry, name)
		}
		if previousName != "" && name <= previousName {
			return nil, fmt.Errorf("%w: manifest order", ErrDuplicateEntry)
		}
		versionValue, err := strconv.ParseUint(matches[1], 10, 64)
		if err != nil || versionValue == 0 {
			return nil, fmt.Errorf("%w: %s", ErrRenamedEntry, name)
		}
		directionKey := matches[1] + "." + matches[3]
		if _, ok := seenDirections[directionKey]; ok {
			return nil, fmt.Errorf("%w: version %s %s", ErrDuplicateEntry, matches[1], matches[3])
		}
		seenNames[name] = struct{}{}
		seenDirections[directionKey] = struct{}{}
		previousName = name
		entries = append(entries, Entry{
			Name: name, Version: uint(versionValue), Direction: matches[3], SHA256: checksum,
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("%w: read manifest", ErrChecksumMismatch)
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("%w: empty manifest", ErrMissingEntry)
	}
	return entries, nil
}

func verifyIdentities(fsys fs.FS, root string, entries []Entry) error {
	body, err := fs.ReadFile(fsys, path.Join(root, IdentityFilename))
	if err != nil {
		return fmt.Errorf("%w: %s", ErrMissingEntry, IdentityFilename)
	}
	if len(body) == 0 || body[len(body)-1] != '\n' {
		return fmt.Errorf("%w: malformed identity baseline", ErrRenamedEntry)
	}

	identities := strings.Split(strings.TrimSuffix(string(body), "\n"), "\n")
	if len(entries) != len(identities)*2 {
		return fmt.Errorf("%w: identity baseline", ErrMissingEntry)
	}
	expectedStems := make(map[uint]string, len(identities))
	for index, identity := range identities {
		matches := migrationIdentity.FindStringSubmatch(identity)
		if matches == nil {
			return fmt.Errorf("%w: malformed identity baseline", ErrRenamedEntry)
		}
		versionValue, err := strconv.ParseUint(matches[1], 10, 64)
		if err != nil || versionValue != uint64(index+1) {
			return fmt.Errorf("%w: identity baseline version %06d", ErrRenamedEntry, index+1)
		}
		expectedStems[uint(versionValue)] = matches[2]
	}
	for _, entry := range entries {
		matches := migrationFilename.FindStringSubmatch(entry.Name)
		if expectedStems[entry.Version] != matches[2] {
			return fmt.Errorf("%w: version %06d", ErrRenamedEntry, entry.Version)
		}
	}
	return nil
}

func verifyFiles(fsys fs.FS, root string, entries []Entry) error {
	manifestNames := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		manifestNames[entry.Name] = struct{}{}
		body, err := fs.ReadFile(fsys, path.Join(root, entry.Name))
		if err != nil {
			return fmt.Errorf("%w: %s", ErrMissingEntry, entry.Name)
		}
		checksum := sha256.Sum256(body)
		if fmt.Sprintf("%x", checksum) != entry.SHA256 {
			return fmt.Errorf("%w: %s", ErrChecksumMismatch, entry.Name)
		}
	}

	dirEntries, err := fs.ReadDir(fsys, root)
	if err != nil {
		return fmt.Errorf("%w: migration directory", ErrMissingEntry)
	}
	for _, dirEntry := range dirEntries {
		if dirEntry.IsDir() || !strings.HasSuffix(dirEntry.Name(), ".sql") {
			continue
		}
		if migrationFilename.FindStringSubmatch(dirEntry.Name()) == nil {
			return fmt.Errorf("%w: %s", ErrRenamedEntry, dirEntry.Name())
		}
		if _, ok := manifestNames[dirEntry.Name()]; !ok {
			return fmt.Errorf("%w: %s", ErrMissingEntry, dirEntry.Name())
		}
	}
	return nil
}

func verifyPairs(entries []Entry) error {
	type pair struct {
		stem       string
		directions map[string]struct{}
	}
	pairs := make(map[uint]*pair)
	for _, entry := range entries {
		matches := migrationFilename.FindStringSubmatch(entry.Name)
		stem := matches[2]
		current, ok := pairs[entry.Version]
		if !ok {
			current = &pair{stem: stem, directions: make(map[string]struct{}, 2)}
			pairs[entry.Version] = current
		}
		if current.stem != stem {
			return fmt.Errorf("%w: version %06d", ErrRenamedEntry, entry.Version)
		}
		current.directions[entry.Direction] = struct{}{}
	}

	versions := make([]int, 0, len(pairs))
	for version := range pairs {
		versions = append(versions, int(version))
	}
	sort.Ints(versions)
	for i, version := range versions {
		if version != i+1 {
			return fmt.Errorf("%w: version %06d", ErrMissingEntry, i+1)
		}
		current := pairs[uint(version)]
		if len(current.directions) != 2 {
			return fmt.Errorf("%w: version %06d pair", ErrMissingEntry, version)
		}
	}
	return nil
}

func isLowerHex(value string) bool {
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}
