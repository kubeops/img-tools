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
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"kmodules.xyz/client-go/tools/parser"

	"github.com/spf13/cobra"
	shell "gomodules.xyz/go-sh"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/yaml"
)

func NewCmdAceUp() *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:                   "ace-up",
		Short:                 "Update ace.yaml",
		DisableFlagsInUseLine: true,
		DisableAutoGenTag:     true,
		RunE: func(cmd *cobra.Command, args []string) error {
			aceImageMap := map[string]string{}
			err := LoadLatestImageMap(filepath.Join(dir, "catalog", "imagelist.yaml"), aceImageMap)
			if err != nil {
				return err
			}

			tagB3, found := aceImageMap["ghcr.io/appscode/b3"]
			if !found {
				return fmt.Errorf("no b3 image found in imagelist.yaml")
			}

			var tagKubeDB string
			featureMap := map[string]string{}
			err = LoadLatestImageMap(filepath.Join(dir, "catalog", "feature-charts.yaml"), featureMap)
			if err != nil {
				return err
			}
			files := []string{
				filepath.Join(dir, "catalog", "imagelist.yaml"),
				fmt.Sprintf("https://github.com/kluster-manager/installer/raw/%s/catalog/imagelist.yaml", tagB3),
			}
			if tag, ok := featureMap["ghcr.io/appscode-charts/kubedb"]; ok {
				tagKubeDB = tag
				files = append(files, fmt.Sprintf("https://github.com/kubedb/installer/raw/%s/catalog/imagelist.yaml", tag))
			}
			if tag, ok := featureMap["ghcr.io/appscode-charts/kubestash"]; ok {
				files = append(files, fmt.Sprintf("https://github.com/kubestash/installer/raw/%s/catalog/imagelist.yaml", tag))
			}
			if tag, ok := featureMap["ghcr.io/appscode-charts/kubevault"]; ok {
				files = append(files, fmt.Sprintf("https://github.com/kubestash/installer/raw/%s/catalog/imagelist.yaml", tag))
			}
			if tag, ok := featureMap["ghcr.io/appscode-charts/capi-catalog"]; ok {
				files = append(files, fmt.Sprintf("https://github.com/kluster-api/installer/raw/%s/catalog/imagelist.yaml", tag))
			}

			var images map[string]string
			if imageList, err := GenerateImageList(files, true); err != nil {
				return err
			} else {
				images = ToImageMap(imageList)
			}
			dbv, err := detectDBVersions(dir)
			if err != nil {
				return err
			}
			err = setDBImages(tagKubeDB, dbv, images)
			if err != nil {
				return err
			}

			// vcluster
			if data, err := os.ReadFile(filepath.Join(dir, "charts", "ace-installer", "values.yaml")); err == nil {
				var vals map[string]any
				err = yaml.Unmarshal(data, &vals)
				if err != nil {
					return err
				}
				tagVCluster, ok, err := unstructured.NestedString(vals, "helm", "releases", "vcluster", "version")
				if err != nil || !ok {
					return fmt.Errorf("no vcluster tag found in charts/ace-installer/values.yaml")
				}
				err = setVClusterImages(tagVCluster, images)
				if err != nil {
					return err
				}
				images["ghcr.io/loft-sh/vcluster-oss"] = images["ghcr.io/loft-sh/vcluster-pro"]

				tagvcp, ok, err := unstructured.NestedString(vals, "helm", "releases", "vcluster-plugin-ace", "version")
				if err != nil || !ok {
					return fmt.Errorf("no vcluster-plugin-ace tag found in charts/ace-installer/values.yaml")
				}
				images["ghcr.io/appscode/vcluster-plugin-ace"] = tagvcp
			}

			aceMap, err := LoadImageMap(filepath.Join(dir, "catalog", "ace.yaml"))
			if err != nil {
				return err
			}

			// update ace with images
			for img, tag := range images {
				tags := aceMap[img]
				switch len(tags) {
				case 0:
					// skip
				case 1:
					aceMap[img] = []string{tag}
				default:
					aceMap[img] = sets.List(sets.New[string](aceMap[img]...).Insert(tag))
				}
			}

			aceMap["ghcr.io/appscode-charts/ace-installer"] = []string{tagB3}
			aceMap["ghcr.io/appscode-charts/ace"] = []string{tagB3}
			aceMap["ghcr.io/appscode-charts/service-gateway"] = []string{tagB3}
			aceMap["ghcr.io/appscode-charts/acaas"] = []string{tagB3}

			return write(ToImageList2(aceMap), filepath.Join(dir, "catalog", "ace.yaml"))
		},
	}

	cmd.Flags().StringVar(&dir, "dir", "", "Directory for appscode-cloud/installer")
	_ = cobra.MarkFlagRequired(cmd.Flags(), "dir")

	return cmd
}

func ToImageList(in map[string]string) []string {
	images := make([]string, 0, len(in))
	for img, tag := range in {
		images = append(images, img+":"+tag)
	}
	return images
}

func ToImageList2(in map[string][]string) []string {
	images := make([]string, 0, len(in))
	for img, tags := range in {
		for _, tag := range tags {
			images = append(images, img+":"+tag)
		}
	}
	sort.Strings(images)
	return images
}

func ToImageMap(in []string) map[string]string {
	images := make(map[string]string, len(in))
	for _, repo := range in {
		if img, tag, ok := strings.Cut(repo, ":"); ok {
			images[img] = tag
		}
	}
	return images
}

type DBVersions struct {
	Postgres string
	Redis    string
}

func detectDBVersions(dir string) (*DBVersions, error) {
	sh := shell.NewSession()
	sh.SetDir(dir)
	sh.ShowCMD = true

	err := sh.SetDir(filepath.Join(dir, "charts", "ace")).Command("helm", "dependency", "update").Run()
	if err != nil {
		return nil, err
	}

	out, err := sh.SetDir(filepath.Join(dir, "charts")).Command("helm", "template", "ace").Output()
	if err != nil {
		return nil, err
	}

	var result DBVersions
	err = parser.ProcessResources(out, func(ri parser.ResourceInfo) error {
		switch ri.Object.GetKind() {
		case "Postgres":
			v, ok, err := unstructured.NestedString(ri.Object.UnstructuredContent(), "spec", "version")
			if err != nil || !ok {
				return fmt.Errorf("postgres version not found")
			}
			result.Postgres = v
		case "Redis":
			v, ok, err := unstructured.NestedString(ri.Object.UnstructuredContent(), "spec", "version")
			if err != nil || !ok {
				return fmt.Errorf("redis version not found")
			}
			result.Redis = v
		}

		return nil
	})
	return &result, err
}

func setDBImages(tag string, dbv *DBVersions, images map[string]string) error {
	sh := shell.NewSession()
	sh.ShowCMD = true

	out, err := sh.Command("helm", "template", "oci://ghcr.io/appscode-charts/kubedb-catalog", fmt.Sprintf("--version=%s", tag)).Output()
	if err != nil {
		return err
	}

	return parser.ProcessResources(out, func(ri parser.ResourceInfo) error {
		obj := ri.Object
		if obj.GetKind() == "PostgresVersion" && obj.GetName() == dbv.Postgres {
			collectImages(obj.UnstructuredContent(), images)
		} else if obj.GetKind() == "RedisVersion" && obj.GetName() == dbv.Redis {
			collectImages(obj.UnstructuredContent(), images)
		}
		return nil
	})
}

func setVClusterImages(tag string, images map[string]string) error {
	sh := shell.NewSession()
	sh.ShowCMD = true

	out, err := sh.Command("helm", "template", "oci://ghcr.io/appscode-charts/vcluster", fmt.Sprintf("--version=%s", tag)).Output()
	if err != nil {
		return err
	}

	return parser.ProcessResources(out, func(ri parser.ResourceInfo) error {
		obj := ri.Object
		collectImages(obj.UnstructuredContent(), images)
		return nil
	})
}

func collectImages(obj map[string]any, images map[string]string) {
	for k, v := range obj {
		if k == "image" {
			if s, ok := v.(string); ok {
				if img, tag, ok := strings.Cut(s, ":"); ok {
					images[img] = tag
				}
			}
		} else if m, ok := v.(map[string]any); ok {
			collectImages(m, images)
		} else if items, ok := v.([]any); ok {
			for _, item := range items {
				if m, ok := item.(map[string]any); ok {
					collectImages(m, images)
				}
			}
		}
	}
}
