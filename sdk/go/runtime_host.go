package etherplugin

func defaultHostForRuntime(opts RemoteHostOptions) Host {
	return NewRemoteHostClient(opts)
}
