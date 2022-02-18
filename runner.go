package main

// Runner interface must implement Run(), Stop() and Initialised() for main
// to be able to orchestrate all runners actions.
type Runner interface {
	Run() error
	Stop()
	Initialised() bool
}
