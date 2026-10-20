package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/imzyb/MiGate/internal/lifecycle"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: migate-manage clean|backup|restore|uninstall --manifest PATH --dry-run")
		os.Exit(2)
	}
	f := flag.NewFlagSet(os.Args[1], flag.ExitOnError)
	manifest := f.String("manifest", "", "installation manifest")
	var o lifecycle.Options
	o.Command = os.Args[1]
	f.BoolVar(&o.DryRun, "dry-run", false, "preview without changes")
	f.BoolVar(&o.Purge, "purge", false, "backup and remove owned data")
	f.BoolVar(&o.WithCores, "with-cores", false, "remove managed cores")
	f.StringVar(&o.Confirm, "confirm", "", "explicit role:operation[:scope]")
	f.StringVar(&o.BackupDir, "backup-dir", "", "external backup directory")
	f.StringVar(&o.PGDump, "pg-dump", "", "pg_dump executable path")
	f.StringVar(&o.PGRestore, "pg-restore", "", "matching pg_restore executable path")
	f.StringVar(&o.Snapshot, "snapshot", "", "absolute path to verified backup snapshot JSON")
	f.Parse(os.Args[2:])
	m, e := lifecycle.Load(*manifest)
	if e == nil && o.Command == "releases" {
		for _, v := range m.Releases {
			fmt.Println(v)
		}
		return
	}
	if e == nil {
		e = lifecycle.Execute(context.Background(), m, o, os.Stdout)
	}
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
