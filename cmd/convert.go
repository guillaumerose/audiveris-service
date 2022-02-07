package main

import (
	"context"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func convert(dir string, input string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/audiveris-extract/bin/Audiveris", "-batch", "-export", "-output", dir, filepath.Join(dir, input))
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	if err := cmd.Run(); err != nil {
		return err
	}
	xml := exec.CommandContext(ctx, "mscore", "-o", "output.xml", "input/input.mxl")
	xml.Dir = dir
	xml.Stderr = os.Stderr
	xml.Stdout = os.Stdout
	if err := xml.Run(); err != nil {
		return err
	}

	bin, err := ioutil.ReadFile(filepath.Join(dir, "output.xml"))
	if err != nil {
		return err
	}
	if err := ioutil.WriteFile(filepath.Join(dir, "output.xml"), []byte(strings.ReplaceAll(string(bin), "[Audiveris detected movement]", "")), 0644); err != nil {
		return err
	}

	mxl := exec.CommandContext(ctx, "mscore", "-o", "output.mxl", "output.xml")
	mxl.Dir = dir
	mxl.Stderr = os.Stderr
	mxl.Stdout = os.Stdout
	if err := mxl.Run(); err != nil {
		return err
	}

	return nil
}
