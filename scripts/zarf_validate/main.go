// Throwaway YAML syntax validator for the zarf/ deliverables. Lives under
// scripts/ so it doesn't get compiled into the production binary set.
// Run with: go run ./scripts/zarf_validate zarf
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

func main() {
	roots := os.Args[1:]
	if len(roots) == 0 {
		roots = []string{"zarf"}
	}
	var files []string
	for _, root := range roots {
		err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				return nil
			}
			if strings.HasSuffix(p, ".yaml") || strings.HasSuffix(p, ".yml") {
				files = append(files, p)
			}
			return nil
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	}
	errs := 0
	for _, f := range files {
		fh, err := os.Open(f)
		if err != nil {
			fmt.Printf("ERR  %s: %v\n", f, err)
			errs++
			continue
		}
		dec := yaml.NewDecoder(fh)
		docs := 0
		for {
			var v interface{}
			if err := dec.Decode(&v); err != nil {
				if err == io.EOF {
					break
				}
				fmt.Printf("ERR  %s (doc %d): %v\n", f, docs, err)
				errs++
				break
			}
			docs++
		}
		fh.Close()
		fmt.Printf("OK   %s  (%d doc(s))\n", f, docs)
	}
	if errs > 0 {
		fmt.Printf("\n%d file(s) failed\n", errs)
		os.Exit(1)
	}
	fmt.Printf("\nAll %d file(s) parsed cleanly\n", len(files))

	// Cross-check Kustomize: every resource referenced by each kustomization.yaml
	// must exist on disk.
	for _, root := range roots {
		_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			if filepath.Base(p) != "kustomization.yaml" {
				return nil
			}
			b, err := os.ReadFile(p)
			if err != nil {
				fmt.Printf("KERR %s: %v\n", p, err)
				errs++
				return nil
			}
			var k struct {
				Resources []string `yaml:"resources"`
				Patches   []struct {
					Path string `yaml:"path"`
				} `yaml:"patches"`
			}
			if err := yaml.Unmarshal(b, &k); err != nil {
				fmt.Printf("KERR %s: %v\n", p, err)
				errs++
				return nil
			}
			dir := filepath.Dir(p)
			for _, r := range k.Resources {
				abs := filepath.Join(dir, r)
				if _, err := os.Stat(abs); err != nil {
					fmt.Printf("KERR %s -> resource %q missing\n", p, r)
					errs++
				}
			}
			for _, pt := range k.Patches {
				abs := filepath.Join(dir, pt.Path)
				if _, err := os.Stat(abs); err != nil {
					fmt.Printf("KERR %s -> patch %q missing\n", p, pt.Path)
					errs++
				}
			}
			fmt.Printf("KOK  %s\n", p)
			return nil
		})
	}
	// Compose validation — services / depends_on / volumes / networks cross-refs.
	for _, root := range roots {
		_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			if filepath.Base(p) != "docker_compose.yaml" {
				return nil
			}
			b, err := os.ReadFile(p)
			if err != nil {
				return nil
			}
			var c struct {
				Services map[string]struct {
					Image     string                 `yaml:"image"`
					DependsOn interface{}            `yaml:"depends_on"`
					Networks  map[string]interface{} `yaml:"networks"`
				} `yaml:"services"`
				Volumes  map[string]interface{} `yaml:"volumes"`
				Networks map[string]interface{} `yaml:"networks"`
			}
			if err := yaml.Unmarshal(b, &c); err != nil {
				fmt.Printf("CERR %s: %v\n", p, err)
				errs++
				return nil
			}
			for name, svc := range c.Services {
				deps := []string{}
				switch d := svc.DependsOn.(type) {
				case []interface{}:
					for _, x := range d {
						if s, ok := x.(string); ok {
							deps = append(deps, s)
						}
					}
				case map[string]interface{}:
					for k := range d {
						deps = append(deps, k)
					}
				}
				for _, dep := range deps {
					if _, ok := c.Services[dep]; !ok {
						fmt.Printf("CERR %s -> service %q depends_on missing %q\n", p, name, dep)
						errs++
					}
				}
				for net := range svc.Networks {
					if _, ok := c.Networks[net]; !ok {
						fmt.Printf("CERR %s -> service %q network %q not declared\n", p, name, net)
						errs++
					}
				}
				if svc.Image == "" {
					fmt.Printf("CWRN %s -> service %q has no image (build-only)\n", p, name)
				}
			}
			fmt.Printf("COK  %s  (%d service(s), %d volume(s))\n", p, len(c.Services), len(c.Volumes))
			return nil
		})
	}
	if errs > 0 {
		os.Exit(1)
	}
}
