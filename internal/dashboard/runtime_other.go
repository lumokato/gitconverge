//go:build !darwin

package dashboard

func refreshRuntime(runtime Runtime) Runtime { return runtime }
