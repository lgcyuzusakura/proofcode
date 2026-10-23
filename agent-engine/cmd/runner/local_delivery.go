package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"time"
)

type localDelivery struct {
	ID    string      `json:"id"`
	Token string      `json:"token"`
	Task  taskMessage `json:"task"`
}

func (r *runner) runLocal(ctx context.Context) error {
	base, err := url.Parse(r.ControlPlane)
	if err != nil || base.Scheme != "http" || base.User != nil || base.RawQuery != "" || base.Fragment != "" || (base.Path != "" && base.Path != "/") {
		return errors.New("local transport requires a loopback HTTP control plane")
	}
	ip := net.ParseIP(base.Hostname())
	if ip == nil || !ip.IsLoopback() {
		return errors.New("local transport requires a loopback HTTP control plane")
	}
	for ctx.Err() == nil {
		delivery, err := r.claimLocalDelivery(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			log.Print("local task delivery unavailable; retrying")
		} else if delivery != nil {
			if err := r.executeLocalDelivery(ctx, *delivery); err != nil && ctx.Err() == nil {
				log.Printf("local delivery %s did not finish", delivery.ID)
			}
			continue
		}
		timer := time.NewTimer(500 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return ctx.Err()
}

func (r *runner) claimLocalDelivery(ctx context.Context) (*localDelivery, error) {
	response, err := r.request(ctx, http.MethodPost, "/internal/local-deliveries/claim", nil)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNoContent {
		return nil, nil
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("local delivery claim returned %d", response.StatusCode)
	}
	var value localDelivery
	if err := json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&value); err != nil {
		return nil, err
	}
	if !taskIDPattern.MatchString(value.ID) || !taskIDPattern.MatchString(value.Token) || !taskIDPattern.MatchString(value.Task.TaskID) || value.Task.Attempt < 1 {
		return nil, errors.New("invalid local task delivery")
	}
	return &value, nil
}

func (r *runner) localReceipt(ctx context.Context, delivery localDelivery, action string) error {
	response, err := r.request(ctx, http.MethodPost, "/internal/local-deliveries/"+delivery.ID+"/"+action, map[string]string{"token": delivery.Token})
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		return fmt.Errorf("local delivery %s returned %d", action, response.StatusCode)
	}
	return nil
}

func (r *runner) executeLocalDelivery(ctx context.Context, delivery localDelivery) error {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan struct{})
	lease := make(chan error, 1)
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				lease <- nil
				return
			case <-runCtx.Done():
				lease <- runCtx.Err()
				return
			case <-ticker.C:
				if err := r.localReceipt(runCtx, delivery, "renew"); err != nil {
					cancel()
					lease <- err
					return
				}
			}
		}
	}()
	runErr := r.runTask(runCtx, delivery.Task)
	close(done)
	leaseErr := <-lease
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if leaseErr != nil {
		return leaseErr
	}
	action := "ack"
	if runErr != nil {
		// The terminal event may have failed to persist. Redelivery lets the
		// existing task claim protocol decide whether execution must resume.
		action = "release"
	}
	if err := r.localReceipt(ctx, delivery, action); err != nil {
		return err
	}
	return runErr
}
