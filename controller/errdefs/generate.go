package errdefs

//go:generate protoc -I=. -I=../../vendor/ --gogo_out=plugins=grpc:. errdefs.proto
