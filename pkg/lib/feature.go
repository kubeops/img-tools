/*
Copyright AppsCode Inc. and Contributors

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

package lib

import (
	"fmt"
	"maps"
	"os"
	"sort"
	"sync"

	shell "gomodules.xyz/go-sh"
	"k8s.io/klog/v2"
	"sigs.k8s.io/yaml"
)

const (
	FeatureChartRegistry = "ghcr.io/appscode-charts"

	featureChartWorkers = 8
)

// FeatureChart is a chart pinned by a Feature or FeatureSet, along with the
// values that resource deploys it with.
type FeatureChart struct {
	Name    string
	Version string
	Values  map[string]any
}

func (c FeatureChart) Ref() string {
	return fmt.Sprintf("%s/%s:%s", FeatureChartRegistry, c.Name, c.Version)
}

// FeatureChartImages renders each feature chart at its pinned version with the
// values its Feature carries and returns the images they reference. Charts that
// fail to render are reported in skipped rather than failing the whole run, so
// that one broken chart can't silently empty the catalog.
func FeatureChartImages(charts []FeatureChart) (images []string, skipped []string, err error) {
	found := map[string]string{}

	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, featureChartWorkers)

	for _, chart := range charts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			chartImages, err := featureChartImages(chart)

			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				klog.Infof("Skipping feature chart %s due to error: %v", chart.Ref(), err)
				skipped = append(skipped, chart.Ref())
				return
			}
			maps.Copy(found, chartImages)
		}()
	}
	wg.Wait()

	sort.Strings(skipped)
	return ListImages(found), skipped, nil
}

func featureChartImages(chart FeatureChart) (map[string]string, error) {
	// No release name: some feature chart names are longer than helm's 53 char
	// release name limit, and the generated name has no bearing on images.
	args := []any{
		"template",
		fmt.Sprintf("oci://%s/%s", FeatureChartRegistry, chart.Name),
		"--version=" + chart.Version,
	}

	if len(chart.Values) > 0 {
		content, err := yaml.Marshal(chart.Values)
		if err != nil {
			return nil, err
		}
		filename, err := writeTempValues(chart.Name, content)
		if err != nil {
			return nil, err
		}
		defer os.Remove(filename) // nolint:errcheck

		args = append(args, "--values="+filename)
	}

	sh := shell.NewSession()
	sh.SetDir(os.TempDir())
	sh.ShowCMD = true

	out, err := sh.Command("helm", args...).Output()
	if err != nil {
		return nil, err
	}

	images := map[string]string{}
	if err := CollectRenderedImages(out, images); err != nil {
		return nil, err
	}
	return images, nil
}
