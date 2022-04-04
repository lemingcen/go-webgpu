package main

import (
	"bytes"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"golang.org/x/exp/slices"
	"modernc.org/cc/v3"
	gofumpt "mvdan.cc/gofumpt/format"
)

var (
	inputFile   string
	outputFile  string
	packageName string
)

func init() {
	flag.StringVar(&inputFile, "i", "", "")
	flag.StringVar(&outputFile, "o", "", "")
	flag.StringVar(&packageName, "pkg", "", "")
}

type Pair struct {
	Enum  string
	Value int64
}

type TypedefEnum struct {
	Name  string
	Enums []Pair
}

type Enums []TypedefEnum

func (e Enums) Add(t string, enum string, value int64) Enums {
	found := false
	for i, v := range e {
		if v.Name == t {
			found = true
			v.Enums = append(v.Enums, Pair{
				Enum:  enum,
				Value: value,
			})
			e[i] = v
		}
	}

	if !found {
		e = append(e, TypedefEnum{
			Name: t,
			Enums: []Pair{{
				Enum:  enum,
				Value: value,
			}},
		})
	}
	return e
}

func main() {
	flag.Parse()

	in := mustv(os.ReadFile(inputFile))

	abi := mustv(cc.NewABI(runtime.GOOS, runtime.GOARCH))
	config := &cc.Config{
		ABI:     abi,
		Config3: cc.Config3{},
	}

	sources := []cc.Source{
		{
			Name:  "defaults.h",
			Value: "#define __STDC_HOSTED__ 1",
		},
		{
			Name:  filepath.Base(inputFile),
			Value: string(in),
		},
	}

	_, includePath, sysIncludePaths, err := cc.HostConfig("cpp")
	must(err)

	includePath = append(includePath, filepath.Dir(inputFile))

	ast := mustv(cc.Parse(config, includePath, sysIncludePaths, sources))
	must(ast.Typecheck())

	var enums = Enums{}

	skipTypes := []string{
		"SType",
		"NativeSType",
	}

loop:
	for k, v := range ast.Enums {
		key := strings.TrimPrefix(k.String(), "WGPU")
		keyS := strings.Split(key, "_")
		typ := keyS[0]
		value := int64(v.Value().(cc.Int64Value))

		for _, v := range skipTypes {
			if typ == v {
				continue loop
			}
		}

		if !strings.HasSuffix(key, "_Force32") {
			enums = enums.Add(typ, key, value)
		}
	}

	slices.SortStableFunc(enums, func(a, b TypedefEnum) bool {
		return a.Name < b.Name
	})

	w := &bytes.Buffer{}

	fmt.Fprintf(w, "// Code generated by github.com/rajveermalviya/go-webgpu/cmd/gen_enums. DO NOT EDIT.\n\n")
	fmt.Fprintf(w, "package %s\n\n", packageName)

	for _, e := range enums {
		slices.SortStableFunc(e.Enums, func(a, b Pair) bool {
			return a.Value < b.Value
		})

		fmt.Fprintf(w, "type %s uint32\n\n", e.Name)
		for _, v := range e.Enums {
			enum := v.Enum
			value := v.Value
			fmt.Fprintf(w, "const %s %s = %d\n", enum, e.Name, value)
		}

		fmt.Fprint(w, "\n")
	}

	out := mustv(os.Create(outputFile))
	mustv(out.Write(fmtFile(w.Bytes())))
	must(out.Close())
}

func fmtFile(b []byte) []byte {
	langVersion := ""
	out, err := exec.Command("go", "list", "-m", "-f", "{{.GoVersion}}").Output()
	outSlice := bytes.Split(out, []byte("\n"))
	out = outSlice[0]
	out = bytes.TrimSpace(out)
	if err == nil && len(out) > 0 {
		langVersion = string(out)
	}

	// Run gofumpt
	b, err = gofumpt.Source(b, gofumpt.Options{LangVersion: langVersion, ExtraRules: true})
	if err != nil {
		log.Fatalf("cannot run gofumpt on file: %v", err)
	}

	return b
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}

func mustv[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}