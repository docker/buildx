package pb

//go:generate protoc -I=. -I=../../vendor/ --gogo_out=plugins=grpc:. controller.proto
