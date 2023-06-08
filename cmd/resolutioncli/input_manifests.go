/*
Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"fmt"
	"os"
	"path/filepath"

	"k8s.io/apimachinery/pkg/runtime"
)

func readManifestFiles(directory string) ([]runtime.Object, error) {
	var objects []runtime.Object

	err := filepath.Walk(directory, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		fileContent, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read file %s: %w", path, err)
		}

		decoder := codecs.UniversalDecoder(scheme.PrioritizedVersionsAllGroups()...)
		object, _, err := decoder.Decode(fileContent, nil, nil)
		if err != nil {
			return fmt.Errorf("failed to decode file %s: %w", path, err)
		}

		objects = append(objects, object)

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to read files: %w", err)
	}

	return objects, nil
}
