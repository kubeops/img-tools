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

package cmds

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"sort"
	"strings"

	"kmodules.xyz/client-go/tools/parser"
	"kmodules.xyz/image-packer/pkg/lib"

	"github.com/spf13/cobra"
	shell "gomodules.xyz/go-sh"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"
)

func NewCmdListFeatureCharts() *cobra.Command {
	var (
		rootDir       string
		outDir        string
		withImages    = true
		excludeCharts []string
	)
	cmd := &cobra.Command{
		Use:                   "list-feature-charts",
		Short:                 "List all feature charts",
		DisableFlagsInUseLine: true,
		DisableAutoGenTag:     true,
		RunE: func(cmd *cobra.Command, args []string) error {
			charts, err := ListUICharts(rootDir)
			if err != nil {
				return err
			}

			refs := sets.New[string]()
			for _, chart := range charts {
				refs.Insert(chart.Ref())
			}
			if err := write(sets.List(refs), filepath.Join(outDir, "feature-charts.yaml")); err != nil {
				return err
			}

			if !withImages {
				return nil
			}

			images, skipped, err := lib.FeatureChartImages(excludeFeatureCharts(charts, excludeCharts))
			if err != nil {
				return err
			}
			if len(skipped) > 0 {
				klog.Warningf("%d feature chart(s) failed to render; their images are missing from feature-chart-images.yaml: %s",
					len(skipped), strings.Join(skipped, ", "))
			}
			return write(images, filepath.Join(outDir, "feature-chart-images.yaml"))
		},
	}

	cmd.Flags().StringVar(&rootDir, "root-dir", "", "Root directory")
	cmd.Flags().StringVar(&outDir, "output-dir", "", "Output directory")
	cmd.Flags().BoolVar(&withImages, "with-images", withImages, "Render each feature chart and write the images it references to feature-chart-images.yaml")
	cmd.Flags().StringSliceVar(&excludeCharts, "exclude-chart", nil, "Feature charts to leave out of feature-chart-images.yaml, by chart name. Use for charts whose images another catalog already publishes. Does not affect feature-charts.yaml")
	_ = cobra.MarkFlagRequired(cmd.Flags(), "output-dir")

	return cmd
}

func excludeFeatureCharts(charts []lib.FeatureChart, exclude []string) []lib.FeatureChart {
	if len(exclude) == 0 {
		return charts
	}

	skip := sets.New[string](exclude...)
	kept := make([]lib.FeatureChart, 0, len(charts))
	matched := sets.New[string]()
	for _, chart := range charts {
		if skip.Has(chart.Name) {
			matched.Insert(chart.Name)
			continue
		}
		kept = append(kept, chart)
	}

	// An exclusion that matches nothing is a stale or misspelled entry in the
	// caller's list, and it would silently start collecting images again.
	if unmatched := skip.Difference(matched); unmatched.Len() > 0 {
		klog.Warningf("%d --exclude-chart value(s) matched no feature chart: %s",
			unmatched.Len(), strings.Join(sets.List(unmatched), ", "))
	}
	klog.Infof("excluded %d feature chart(s) from the image list", matched.Len())

	return kept
}

type Skeleton struct {
	Spec struct {
		Chart struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"chart"`
	} `json:"spec"`
}

type ChartInfo struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	AppVersion  string `json:"app_version"`
	Description string `json:"description"`
}

func ListUICharts(rootDir string) ([]lib.FeatureChart, error) {
	sh := shell.NewSession()
	sh.SetDir("/tmp")
	sh.ShowCMD = true

	var out []byte
	var err error

	if rootDir == "" {
		// helm search repo opscenter-features -o json
		out, err = sh.Command("helm", "search", "repo", "opscenter-features", "-o", "json").Output()
		if err != nil {
			return nil, err
		}
		var list []ChartInfo
		err = json.Unmarshal(out, &list)
		if err != nil {
			return nil, err
		}
		if len(list) == 0 {
			return nil, errors.New("helm chart opscenter-features not found")
		}
		version := list[0].Version

		out, err = sh.Command("helm", "template", "oci://ghcr.io/appscode-charts/opscenter-features", "--version="+version).Output()
		if err != nil {
			return nil, err
		}
	} else {
		out, err = sh.SetDir(rootDir).Command("helm", "template", "opscenter-features").Output()
		if err != nil {
			return nil, err
		}
	}

	helmout, err := parser.ListResources(out)
	if err != nil {
		panic(err)
	}

	var charts []lib.FeatureChart
	// A chart can be pinned by several Features; the same chart deployed with
	// different values can pull different images, so dedup on values too.
	seen := sets.New[string]()
	for _, ri := range helmout {
		if ri.Object.GetKind() != "FeatureSet" && ri.Object.GetKind() != "Feature" {
			continue
		}

		chartName, found, err := unstructured.NestedString(ri.Object.UnstructuredContent(), "spec", "chart", "name")
		if err != nil {
			return nil, err
		} else if !found {
			continue
		}
		chartVersion, found, err := unstructured.NestedString(ri.Object.UnstructuredContent(), "spec", "chart", "version")
		if err != nil {
			return nil, err
		} else if !found {
			continue
		}
		values, _, err := unstructured.NestedMap(ri.Object.UnstructuredContent(), "spec", "values")
		if err != nil {
			return nil, err
		}

		chart := lib.FeatureChart{
			Name:    chartName,
			Version: chartVersion,
			Values:  values,
		}
		key, err := json.Marshal([]any{chart.Ref(), values})
		if err != nil {
			return nil, err
		}
		if seen.Has(string(key)) {
			continue
		}
		seen.Insert(string(key))
		charts = append(charts, chart)
	}

	sort.Slice(charts, func(i, j int) bool { return charts[i].Ref() < charts[j].Ref() })
	return charts, nil
}
