package migrationbundle

import (
	"errors"
	"testing"
	"testing/fstest"
)

const (
	testUpOne   = "CREATE TABLE test (id integer);\n"
	testDownOne = "DROP TABLE test;\n"
	testUpTwo   = "ALTER TABLE test ADD COLUMN name text;\n"
	testDownTwo = "ALTER TABLE test DROP COLUMN name;\n"
)

func TestVerifyReturnsDeterministicOrderedManifest(t *testing.T) {
	fsys := validTestBundle()

	manifest, err := Verify(fsys, ".")
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}

	wantNames := []string{
		"000001_test.down.sql",
		"000001_test.up.sql",
		"000002_add_name.down.sql",
		"000002_add_name.up.sql",
	}
	if len(manifest.Entries) != len(wantNames) {
		t.Fatalf("entry count = %d, want %d", len(manifest.Entries), len(wantNames))
	}
	for i, want := range wantNames {
		if got := manifest.Entries[i].Name; got != want {
			t.Errorf("entry %d name = %q, want %q", i, got, want)
		}
	}
	if manifest.LatestVersion != 2 {
		t.Errorf("latest version = %d, want 2", manifest.LatestVersion)
	}
	if manifest.Digest != "97d48cb4eb69aa4a94876dabb6bcb7e68f259bc1971fb6ca843f411badd4f2f3" {
		t.Errorf("manifest digest = %q, want deterministic fixture digest", manifest.Digest)
	}
}

func TestVerifyRejectsInvalidBundleEntries(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(fstest.MapFS)
		wantErr error
	}{
		{
			name: "duplicate manifest entry",
			mutate: func(fsys fstest.MapFS) {
				fsys[ManifestFilename].Data = append(
					fsys[ManifestFilename].Data,
					[]byte("30d16a80498b1d62b4b13130c046b82dedf340b99cdb15ba0d2500a7e6a102be  000001_test.up.sql\n")...,
				)
			},
			wantErr: ErrDuplicateEntry,
		},
		{
			name: "missing SQL file",
			mutate: func(fsys fstest.MapFS) {
				delete(fsys, "000002_add_name.up.sql")
			},
			wantErr: ErrMissingEntry,
		},
		{
			name: "SQL file missing from manifest",
			mutate: func(fsys fstest.MapFS) {
				fsys["000003_unlisted.up.sql"] = &fstest.MapFile{Data: []byte("SELECT 1;\n")}
			},
			wantErr: ErrMissingEntry,
		},
		{
			name: "checksum mismatch",
			mutate: func(fsys fstest.MapFS) {
				fsys["000001_test.up.sql"].Data = []byte("CREATE TABLE changed (id integer);\n")
			},
			wantErr: ErrChecksumMismatch,
		},
		{
			name: "duplicate version and direction",
			mutate: func(fsys fstest.MapFS) {
				fsys["000001_duplicate.up.sql"] = &fstest.MapFile{Data: []byte(testUpOne)}
				fsys[ManifestFilename].Data = append(
					fsys[ManifestFilename].Data,
					[]byte("30d16a80498b1d62b4b13130c046b82dedf340b99cdb15ba0d2500a7e6a102be  000001_duplicate.up.sql\n")...,
				)
			},
			wantErr: ErrDuplicateEntry,
		},
		{
			name: "renamed migration pair",
			mutate: func(fsys fstest.MapFS) {
				delete(fsys, "000002_add_name.down.sql")
				fsys["000002_old_name.down.sql"] = &fstest.MapFile{Data: []byte(testDownTwo)}
				fsys[ManifestFilename].Data = []byte(
					"5111d07169d0ba3c9f4c861fa6076c786f86469e298450c641c3e70ea21df8f6  000001_test.down.sql\n" +
						"30d16a80498b1d62b4b13130c046b82dedf340b99cdb15ba0d2500a7e6a102be  000001_test.up.sql\n" +
						"ce9d98e373b52335a1f1a4dfbfae88940a58ee177c2f74d34fa557caf4bfe3db  000002_add_name.up.sql\n" +
						"b984852a927d0b678f00dd889b89331e69002fa7ec56eacdcbc869a4ebc252e9  000002_old_name.down.sql\n",
				)
			},
			wantErr: ErrRenamedEntry,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fsys := validTestBundle()
			tt.mutate(fsys)

			_, err := Verify(fsys, ".")
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Verify() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func validTestBundle() fstest.MapFS {
	return fstest.MapFS{
		"000001_test.up.sql":       &fstest.MapFile{Data: []byte(testUpOne)},
		"000001_test.down.sql":     &fstest.MapFile{Data: []byte(testDownOne)},
		"000002_add_name.up.sql":   &fstest.MapFile{Data: []byte(testUpTwo)},
		"000002_add_name.down.sql": &fstest.MapFile{Data: []byte(testDownTwo)},
		ManifestFilename: &fstest.MapFile{Data: []byte(
			"5111d07169d0ba3c9f4c861fa6076c786f86469e298450c641c3e70ea21df8f6  000001_test.down.sql\n" +
				"30d16a80498b1d62b4b13130c046b82dedf340b99cdb15ba0d2500a7e6a102be  000001_test.up.sql\n" +
				"b984852a927d0b678f00dd889b89331e69002fa7ec56eacdcbc869a4ebc252e9  000002_add_name.down.sql\n" +
				"ce9d98e373b52335a1f1a4dfbfae88940a58ee177c2f74d34fa557caf4bfe3db  000002_add_name.up.sql\n",
		)},
	}
}
