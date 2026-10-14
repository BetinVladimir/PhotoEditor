package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"photoeditor/backend/internal/workflow"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type options struct {
	node    string
	timeout time.Duration
	poll    time.Duration
}

func parseOptions(args []string) (options, error) {
	var out options
	flags := flag.NewFlagSet("capacity-drain", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	flags.DurationVar(&out.timeout, "timeout", 7260*time.Second, "maximum drain wait")
	flags.DurationVar(&out.poll, "poll", 2*time.Second, "status poll interval")
	if err := flags.Parse(args); err != nil {
		return options{}, err
	}
	if flags.NArg() != 1 || len(flags.Arg(0)) < 1 || len(flags.Arg(0)) > 120 ||
		out.timeout <= 0 || out.timeout > 7260*time.Second ||
		out.poll <= 0 || out.poll > 30*time.Second {
		return options{}, errors.New("usage: capacity-drain [--timeout=2h1m] [--poll=2s] NODE_ID")
	}
	out.node = flags.Arg(0)
	return out, nil
}

func run(ctx context.Context, args []string, dsn string) error {
	options, err := parseOptions(args)
	if err != nil {
		return err
	}
	if dsn == "" {
		return errors.New("DATABASE_URL required")
	}
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return errors.New("invalid scheduler database configuration")
	}
	config.ConnConfig.RuntimeParams["role"] = "photo_scheduler"
	config.ConnConfig.RuntimeParams["statement_timeout"] = "5000"
	config.ConnConfig.RuntimeParams["lock_timeout"] = "3000"
	config.MaxConns = 1
	wait, cancel := context.WithTimeout(ctx, options.timeout)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(wait, config)
	if err != nil {
		return errors.New("scheduler database unavailable")
	}
	defer pool.Close()
	var role string
	if err = pool.QueryRow(wait, "SELECT current_user").Scan(&role); err != nil || role != "photo_scheduler" {
		return errors.New("scheduler database role unavailable")
	}
	drainer := workflow.SQLNodeDrainer{DB: pool}
	generation, err := drainer.Begin(wait, options.node)
	if err != nil {
		return err
	}
	if _, err = drainer.WaitReady(wait, options.node, generation, options.poll); err != nil {
		return err
	}
	status, err := drainer.Status(wait, options.node, generation)
	if err != nil || !status.ReadyToStop {
		return errors.New("node drain proof unavailable")
	}
	return json.NewEncoder(os.Stdout).Encode(struct {
		Node       string `json:"node_id"`
		Generation int64  `json:"drain_generation"`
		Ready      bool   `json:"ready_to_stop"`
	}{status.Node, status.Generation, true})
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Getenv("DATABASE_URL")); err != nil {
		fmt.Fprintln(os.Stderr, "capacity-drain:", err)
		os.Exit(1)
	}
}
