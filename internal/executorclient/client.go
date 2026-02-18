package executorclient

import (
	"context"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/lzjever/mbos-wvs/gen/go/executor/v1"
)

type Client struct {
	conn   *grpc.ClientConn
	client pb.ExecutorServiceClient
}

func New(addr string) (*Client, error) {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("dial executor %s: %w", addr, err)
	}
	return &Client{conn: conn, client: pb.NewExecutorServiceClient(conn)}, nil
}

func (c *Client) ExecuteTask(ctx context.Context, req *pb.ExecuteTaskRequest) (*pb.ExecuteTaskResponse, error) {
	return c.client.ExecuteTask(ctx, req)
}

func (c *Client) Close() error {
	return c.conn.Close()
}
