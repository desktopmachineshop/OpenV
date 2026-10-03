package main

// maxRequestBodyBytes is the cap the API places on any single request body.
// OPENV_MAX_BODY_MB overrides the 32 MB default; attachment uploads carry a
// tighter cap of their own (OPENV_MAX_UPLOAD_MB).
func maxRequestBodyBytes() int64 {
	return int64(envInt("OPENV_MAX_BODY_MB", 32)) * 1024 * 1024
}
