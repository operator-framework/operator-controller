package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"
)

func main() {
	var kubeVersionString, kindModPath string

	var rootCmd = &cobra.Command{
		Use:   "k8saligner",
		Short: "Compares Kubernetes version with kind image version",
		RunE: func(cmd *cobra.Command, args []string) error {
			kindVersion, err := parseKindVersion(kindModPath)
			if err != nil {
				return fmt.Errorf("failed to parse kind version: %w", err)
			}

			imageVersion, err := fetchKindImageVersion(kindVersion)
			if err != nil {
				return fmt.Errorf("failed to fetch kind image version: %w", err)
			}

			kubeVersion, err := semver.NewVersion(kubeVersionString)
			if err != nil {
				return fmt.Errorf("invalid Kubernetes version format: %w", err)
			}
			match := ((kubeVersion.Major() == imageVersion.Major()) && (kubeVersion.Minor() == imageVersion.Minor()))

			fmt.Printf("Kubernetes version: %s\nKind image version: %s\n", kubeVersion, imageVersion)
			if match {
				fmt.Println("major and minor versions match.")
				os.Exit(0)
			} else {
				fmt.Println("major and minor versions do NOT match.")
				os.Exit(1)
			}
			return nil
		},
	}

	rootCmd.Flags().StringVar(&kubeVersionString, "kube-version", "", "Kubernetes version string (e.g. v1.29.0)")
	rootCmd.Flags().StringVar(&kindModPath, "kind-mod-path", "", "Path to .bingo/kind.mod file")
	_ = rootCmd.MarkFlagRequired("kube-version")
	_ = rootCmd.MarkFlagRequired("kind-mod-path")

	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func parseKindVersion(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	re := regexp.MustCompile(`require sigs\.k8s\.io/kind\s+([^\s]+)`)
	for scanner.Scan() {
		line := scanner.Text()
		if m := re.FindStringSubmatch(line); m != nil {
			fmt.Printf("Found kind version: %s\n", m[1])
			return m[1], nil
		}
	}
	return "", errors.New("kind version not found in mod file")
}

func fetchKindImageVersion(kindVersion string) (*semver.Version, error) {
	url := fmt.Sprintf("https://github.com/kubernetes-sigs/kind/raw/refs/tags/%s/pkg/apis/config/defaults/image.go", kindVersion)
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("failed to fetch file: %s", resp.Status)
	}
	return extractImageVersion(resp.Body)
}

func extractImageVersion(r io.Reader) (*semver.Version, error) {
	scanner := bufio.NewScanner(r)
	re := regexp.MustCompile(`^const\s+Image\s+=\s+"[^:]+:?(.*)"`)
	for scanner.Scan() {
		line := scanner.Text()
		if m := re.FindStringSubmatch(line); m != nil {
			tagString := strings.Split(m[1], "@")[0] // floating tag part before any hash
			v, err := semver.NewVersion(tagString)
			if err != nil {
				return nil, fmt.Errorf("invalid version format: %s", m[1])
			}
			return v, nil
		}
	}
	return nil, errors.New("image constant not found")
}
