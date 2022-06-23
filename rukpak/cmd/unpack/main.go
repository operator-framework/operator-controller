package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/operator-framework/rukpak/internal/version"
)

func main() {
	var bundleDir string
	var rukpakVersion bool

	skipRootPaths := sets.NewString(
		"/dev",
		"/etc",
		"/proc",
		"/product_name",
		"/product_uuid",
		"/sys",
		"/bin",
	)
	cmd := &cobra.Command{
		Use:  "unpack",
		Args: cobra.ExactArgs(0),
		RunE: func(cmd *cobra.Command, _ []string) error {
			if rukpakVersion {
				fmt.Printf("Git commit: %s\n", version.String())
				os.Exit(0)
			}
			var err error
			bundleDir, err = filepath.Abs(bundleDir)
			if err != nil {
				log.Fatalf("get absolute path of bundle directory %q: %v", bundleDir, err)
			}

			bundleFS := os.DirFS(bundleDir)
			buf := &bytes.Buffer{}
			gzw := gzip.NewWriter(buf)
			tw := tar.NewWriter(gzw)
			if err := fs.WalkDir(bundleFS, ".", func(path string, d fs.DirEntry, err error) error {
				if err != nil {
					return err
				}

				if d.Type()&os.ModeSymlink != 0 {
					return nil
				}
				if bundleDir == "/" {
					// If bundleDir is the filesystem root, skip some known unrelated directories
					fullPath := filepath.Join(bundleDir, path)
					if skipRootPaths.Has(fullPath) {
						return filepath.SkipDir
					}
				}
				info, err := d.Info()
				if err != nil {
					return fmt.Errorf("get file info for %q: %v", path, err)
				}

				h, err := tar.FileInfoHeader(info, "")
				if err != nil {
					return fmt.Errorf("build tar file info header for %q: %v", path, err)
				}
				h.Uid = 0
				h.Gid = 0
				h.Uname = ""
				h.Gname = ""
				h.Name = path

				if err := tw.WriteHeader(h); err != nil {
					return fmt.Errorf("write tar header for %q: %v", path, err)
				}
				if d.IsDir() {
					return nil
				}
				f, err := bundleFS.Open(path)
				if err != nil {
					return fmt.Errorf("open file %q: %v", path, err)
				}
				if _, err := io.Copy(tw, f); err != nil {
					return fmt.Errorf("write tar data for %q: %v", path, err)
				}
				return nil
			}); err != nil {
				log.Fatalf("generate tar.gz for bundle dir %q: %v", bundleDir, err)
			}
			if err := tw.Close(); err != nil {
				log.Fatal(err)
			}
			if err := gzw.Close(); err != nil {
				log.Fatal(err)
			}

			bundleMap := map[string]interface{}{
				"content": buf.Bytes(),
			}
			enc := json.NewEncoder(os.Stdout)
			if err := enc.Encode(bundleMap); err != nil {
				log.Fatalf("encode bundle map as JSON: %v", err)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&bundleDir, "bundle-dir", "", "directory in which the bundle can be found")
	cmd.Flags().BoolVar(&rukpakVersion, "version", false, "displays rukpak version information")

	if err := cmd.Execute(); err != nil {
		log.Fatal(err)
	}
}
