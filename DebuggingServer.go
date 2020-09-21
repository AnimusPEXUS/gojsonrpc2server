package gojsonrpc2server

// Just like Server, but with debugging commands preinstalled

type DebuggingServerOptions struct {
	ServerOptions
	DebugResultsOutputDir string
	FileNamesPrefix       string
	FileNamesSufix        string
}

type DebuggingServer struct {
	*Server
	options *DebuggingServerOptions
}

func NewDebuggingServer(options *DebuggingServerOptions) (*DebuggingServer, error) {

	self := &DebuggingServer{
		options: options,
	}

	s, err := NewServer(&self.options.ServerOptions)
	if err != nil {
		return nil, err
	}

	self.Server = s

	return self, nil
}
