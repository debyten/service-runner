package servicerunner

import (
	"context"
	"errors"
	"github.com/oklog/run"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
)

// SigContext return a context and a cancellation function which is invoked on the following
// system signals:
//
//	SIGKILL
//	SIGTERM
//	SIGINT
func SigContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt)
}

// Closer wraps a `func() error` to return a `io.Closer`
func Closer(fn func() error) io.Closer {
	return closer{closeFn: fn}
}

type closer struct {
	closeFn func() error
}

func (c closer) Close() error {
	return c.closeFn()
}

type RunFn func(ctx context.Context)

// Runner is a simple helper to run a microservice.
//
// Usually the built services exposes a set of HTTP api endpoints.
//
//	Usage:
//	logger := log.NewJSONLogger(os.Stdout)
//	addr := addresses.NewAddressWithHost("service-ServerName", "localhost", "8080")
//	runner.New(addr, logger).
//	  AddCleanups(database, redis, ...).
//	  Run(mux)
type Runner interface {
	// Run will listen and serve with the given http.Handler.
	// When one of these syscall is received, the service will be stopped.
	//
	//  Stop signals: syscall.SIGINT, syscall.SIGTERM, syscall.SIGKILL
	Run(ctx context.Context, mu http.Handler)
	// Async starts only the runners without the http.Handler
	Async(ctx context.Context)
	// OnClose will add a cleanup function to be called when the runner is stopped.
	OnClose(cleanup ...io.Closer) Runner
	// AddRunner adds a func(ctx) that will be executed when invoking Run (in a separate goroutine).
	//
	// NOTE: if a RunFn return an error or exits because its work is done, all the underlying runners
	// and the main server (if any) are halted.
	AddRunner(runners ...RunFn) Runner
}

// NewEnv return a Runner with NewEnvConfig.
func NewEnv(logger *slog.Logger, testing ...bool) Runner {
	cfg := NewEnvConfig()
	return New(cfg, logger, testing...)
}

// New initializes a new Runner.
func New(serverConfig Config, logger *slog.Logger, testing ...bool) Runner {
	forTesting := false
	if len(testing) == 1 && testing[0] {
		forTesting = true
	}
	return &runner{
		Group:        &run.Group{},
		serverConfig: serverConfig,
		logger:       logger,
		runners:      make([]RunFn, 0),
		cleanup:      make([]io.Closer, 0),
		forTesting:   forTesting,
	}
}

type runner struct {
	*run.Group
	serverConfig Config
	logger       *slog.Logger
	runners      []RunFn
	cleanup      []io.Closer

	forTesting bool
}

func (r *runner) AddRunner(runners ...RunFn) Runner {
	r.runners = append(r.runners, runners...)
	return r
}

func (r *runner) OnClose(cleanup ...io.Closer) Runner {
	r.cleanup = append(r.cleanup, cleanup...)
	return r
}

func (r *runner) startHttpServer(mu http.Handler) io.Closer {
	if r.forTesting {
		return io.NopCloser(nil)
	}
	a := r.serverConfig
	srv := &http.Server{Addr: a.addr(), Handler: mu}
	r.Add(func() error {
		if err := srv.ListenAndServe(); err != nil {
			if !errors.Is(err, http.ErrServerClosed) {
				return err
			}
		}
		return nil
	}, func(err error) {
		if err != nil {
			r.logger.Error("http server failure", "service", r.serverConfig.Name(), "err", err)
		}
	})
	return srv
}

func (r *runner) Async(ctx context.Context) {
	for _, runFn := range r.runners {
		r.Add(func() error {
			runFn(ctx)
			return nil
		}, func(error) {

		})
	}
	keyValues := make([]any, 0)
	if r.forTesting {
		keyValues = append(keyValues, "TESTING-MODE", "ON")
	}
	r.logger.Debug("async runner started", keyValues...)
	if err := r.Group.Run(); err != nil {
		r.logger.Error("group runner failure", "mode", "async", "err", err)
		os.Exit(1)
	}
}

func (r *runner) Run(ctx context.Context, mu http.Handler) {
	srv := r.startHttpServer(mu)
	{
		r.Add(func() error {
			<-ctx.Done()
			_ = srv.Close()
			for _, closer := range r.cleanup {
				_ = closer.Close()
			}
			return nil
		}, func(err error) {})
	}
	for _, runFn := range r.runners {
		r.Add(func() error {
			runFn(ctx)
			return nil
		}, func(error) {

		})
	}
	keyValues := make([]any, 0)
	if r.forTesting {
		keyValues = append(keyValues, "TESTING-MODE", "ON")
	}
	r.logger.Debug("group runner start", keyValues...)
	err := r.Group.Run()
	if err != nil {
		r.logger.Error("group runner failure", "mode", "sync", "err", err)
		os.Exit(1)
	}
}
