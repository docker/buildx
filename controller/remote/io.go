package remote

import (
	"context"
	"io"
	"syscall"
	"time"

	"github.com/docker/buildx/controller/pb"
	"github.com/moby/sys/signal"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"
)

type msgStream interface {
	Send(*pb.Message) error
	Recv() (*pb.Message, error)
}

type ioServerConfig struct {
	stdin          io.WriteCloser
	stdout, stderr io.ReadCloser

	// signalFn is a callback function called when a signal is reached to the client.
	signalFn func(context.Context, syscall.Signal) error

	// resizeFn is a callback function called when a resize event is reached to the client.
	resizeFn func(context.Context, winSize) error
}

func serveIO(attachCtx context.Context, srv msgStream, initFn func(*pb.InitMessage) error, ioConfig *ioServerConfig) (err error) {
	stdin, stdout, stderr := ioConfig.stdin, ioConfig.stdout, ioConfig.stderr
	stream := &debugStream{srv, "server=" + time.Now().String()}
	eg, ctx := errgroup.WithContext(attachCtx)
	done := make(chan struct{})

	msg, err := receive(ctx, stream)
	if err != nil {
		return err
	}
	init := msg.GetInit()
	if init == nil {
		return errors.Errorf("unexpected message: %T; wanted init", msg.GetInput())
	}
	sessionID := init.SessionID
	if sessionID == "" {
		return errors.New("no session ID is provided")
	}
	if err := initFn(init); err != nil {
		return errors.Wrap(err, "failed to initialize IO server")
	}

	if stdout != nil {
		stdoutReader, stdoutWriter := io.Pipe()
		eg.Go(func() error {
			<-done
			return stdoutWriter.Close()
		})

		go func() {
			// do not wait for read completion but return here and let the caller send EOF
			// this allows us to return on ctx.Done() without being blocked by this reader.
			io.Copy(stdoutWriter, stdout)
			stdoutWriter.Close()
		}()

		eg.Go(func() error {
			defer stdoutReader.Close()
			return copyToStream(1, stream, stdoutReader)
		})
	}

	if stderr != nil {
		stderrReader, stderrWriter := io.Pipe()
		eg.Go(func() error {
			<-done
			return stderrWriter.Close()
		})

		go func() {
			// do not wait for read completion but return here and let the caller send EOF
			// this allows us to return on ctx.Done() without being blocked by this reader.
			io.Copy(stderrWriter, stderr)
			stderrWriter.Close()
		}()

		eg.Go(func() error {
			defer stderrReader.Close()
			return copyToStream(2, stream, stderrReader)
		})
	}

	msgCh := make(chan *pb.Message)
	eg.Go(func() error {
		defer close(msgCh)
		for {
			msg, err := receive(ctx, stream)
			if err != nil {
				return err
			}
			select {
			case msgCh <- msg:
			case <-done:
				return nil
			case <-ctx.Done():
				return nil
			}
		}
	})

	eg.Go(func() error {
		defer close(done)
		for {
			var msg *pb.Message
			select {
			case msg = <-msgCh:
			case <-ctx.Done():
				return nil
			}
			if msg == nil {
				return nil
			}
			if file := msg.GetFile(); file != nil {
				if file.Fd != 0 {
					return errors.Errorf("unexpected fd: %v", file.Fd)
				}
				if stdin == nil {
					continue // no stdin destination is specified so ignore the data
				}
				if len(file.Data) > 0 {
					_, err := stdin.Write(file.Data)
					if err != nil {
						return err
					}
				}
				if file.EOF {
					stdin.Close()
				}
			} else if resize := msg.GetResize(); resize != nil {
				if ioConfig.resizeFn != nil {
					ioConfig.resizeFn(ctx, winSize{
						cols: resize.Cols,
						rows: resize.Rows,
					})
				}
			} else if sig := msg.GetSignal(); sig != nil {
				if ioConfig.signalFn != nil {
					syscallSignal, ok := signal.SignalMap[sig.Name]
					if !ok {
						continue
					}
					ioConfig.signalFn(ctx, syscallSignal)
				}
			} else {
				return errors.Errorf("unexpected message: %T", msg.GetInput())
			}
		}
	})

	return eg.Wait()
}

type ioAttachConfig struct {
	stdin          io.ReadCloser
	stdout, stderr io.WriteCloser
	signal         <-chan syscall.Signal
	resize         <-chan winSize
}

type winSize struct {
	rows uint32
	cols uint32
}

func attachIO(ctx context.Context, stream msgStream, initMessage *pb.InitMessage, cfg ioAttachConfig) (retErr error) {
	eg, ctx := errgroup.WithContext(ctx)
	done := make(chan struct{})

	if err := stream.Send(&pb.Message{
		Input: &pb.Message_Init{
			Init: initMessage,
		},
	}); err != nil {
		return errors.Wrap(err, "failed to init")
	}

	if cfg.stdin != nil {
		stdinReader, stdinWriter := io.Pipe()
		eg.Go(func() error {
			<-done
			return stdinWriter.Close()
		})

		go func() {
			// do not wait for read completion but return here and let the caller send EOF
			// this allows us to return on ctx.Done() without being blocked by this reader.
			io.Copy(stdinWriter, cfg.stdin)
			stdinWriter.Close()
		}()

		eg.Go(func() error {
			defer stdinReader.Close()
			return copyToStream(0, stream, stdinReader)
		})
	}

	if cfg.signal != nil {
		eg.Go(func() error {
			names := signalNames()
			for {
				var sig syscall.Signal
				select {
				case sig = <-cfg.signal:
				case <-done:
					return nil
				case <-ctx.Done():
					return nil
				}
				name := names[sig]
				if name == "" {
					continue
				}
				if err := stream.Send(&pb.Message{
					Input: &pb.Message_Signal{
						Signal: &pb.SignalMessage{
							Name: name,
						},
					},
				}); err != nil {
					return errors.Wrap(err, "failed to send signal")
				}
			}
		})
	}

	if cfg.resize != nil {
		eg.Go(func() error {
			for {
				var win winSize
				select {
				case win = <-cfg.resize:
				case <-done:
					return nil
				case <-ctx.Done():
					return nil
				}
				if err := stream.Send(&pb.Message{
					Input: &pb.Message_Resize{
						Resize: &pb.ResizeMessage{
							Rows: win.rows,
							Cols: win.cols,
						},
					},
				}); err != nil {
					return errors.Wrap(err, "failed to send resize")
				}
			}
		})
	}

	msgCh := make(chan *pb.Message)
	eg.Go(func() error {
		defer close(msgCh)
		for {
			msg, err := receive(ctx, stream)
			if err != nil {
				return err
			}
			select {
			case msgCh <- msg:
			case <-done:
				return nil
			case <-ctx.Done():
				return nil
			}
		}
	})

	eg.Go(func() error {
		eofs := make(map[uint32]struct{})
		defer close(done)
		for {
			var msg *pb.Message
			select {
			case msg = <-msgCh:
			case <-ctx.Done():
				return nil
			}
			if msg == nil {
				return nil
			}
			if file := msg.GetFile(); file != nil {
				if _, ok := eofs[file.Fd]; ok {
					continue
				}
				var out io.WriteCloser
				switch file.Fd {
				case 1:
					out = cfg.stdout
				case 2:
					out = cfg.stderr
				default:
					return errors.Errorf("unsupported fd %d", file.Fd)
				}
				if out == nil {
					logrus.Warnf("attachIO: no writer for fd %d", file.Fd)
					continue
				}
				if len(file.Data) > 0 {
					if _, err := out.Write(file.Data); err != nil {
						return err
					}
				}
				if file.EOF {
					eofs[file.Fd] = struct{}{}
				}
			} else {
				return errors.Errorf("unexpected message: %T", msg.GetInput())
			}
		}
	})

	return eg.Wait()
}

func receive(ctx context.Context, stream msgStream) (*pb.Message, error) {
	msgCh := make(chan *pb.Message)
	errCh := make(chan error)
	go func() {
		msg, err := stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return
			}
			errCh <- err
			return
		}
		msgCh <- msg
	}()
	select {
	case msg := <-msgCh:
		return msg, nil
	case err := <-errCh:
		return nil, err
	case <-ctx.Done():
		return nil, context.Cause(ctx)
	}
}

func copyToStream(fd uint32, snd msgStream, r io.Reader) error {
	for {
		buf := make([]byte, 32*1024)
		n, err := r.Read(buf)
		if err != nil {
			if err == io.EOF {
				break // break loop and send EOF
			}
			return err
		} else if n > 0 {
			if err := snd.Send(&pb.Message{
				Input: &pb.Message_File{
					File: &pb.FdMessage{
						Fd:   fd,
						Data: buf[:n],
					},
				},
			}); err != nil {
				return err
			}
		}
	}
	return snd.Send(&pb.Message{
		Input: &pb.Message_File{
			File: &pb.FdMessage{
				Fd:  fd,
				EOF: true,
			},
		},
	})
}

func signalNames() map[syscall.Signal]string {
	m := make(map[syscall.Signal]string, len(signal.SignalMap))
	for name, value := range signal.SignalMap {
		m[value] = name
	}
	return m
}

type debugStream struct {
	msgStream
	prefix string
}

func (s *debugStream) Send(msg *pb.Message) error {
	switch m := msg.GetInput().(type) {
	case *pb.Message_File:
		if m.File.EOF {
			logrus.Debugf("|---> File Message (sender:%v) fd=%d, EOF", s.prefix, m.File.Fd)
		} else {
			logrus.Debugf("|---> File Message (sender:%v) fd=%d, %d bytes", s.prefix, m.File.Fd, len(m.File.Data))
		}
	case *pb.Message_Resize:
		logrus.Debugf("|---> Resize Message (sender:%v): %+v", s.prefix, m.Resize)
	case *pb.Message_Signal:
		logrus.Debugf("|---> Signal Message (sender:%v): %s", s.prefix, m.Signal.Name)
	}
	return s.msgStream.Send(msg)
}

func (s *debugStream) Recv() (*pb.Message, error) {
	msg, err := s.msgStream.Recv()
	if err != nil {
		return nil, err
	}
	switch m := msg.GetInput().(type) {
	case *pb.Message_File:
		if m.File.EOF {
			logrus.Debugf("|<--- File Message (receiver:%v) fd=%d, EOF", s.prefix, m.File.Fd)
		} else {
			logrus.Debugf("|<--- File Message (receiver:%v) fd=%d, %d bytes", s.prefix, m.File.Fd, len(m.File.Data))
		}
	case *pb.Message_Resize:
		logrus.Debugf("|<--- Resize Message (receiver:%v): %+v", s.prefix, m.Resize)
	case *pb.Message_Signal:
		logrus.Debugf("|<--- Signal Message (receiver:%v): %s", s.prefix, m.Signal.Name)
	}
	return msg, nil
}
