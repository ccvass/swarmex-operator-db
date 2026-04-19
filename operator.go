package operatordb

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/client"
)

const (
	labelEnabled    = "swarmex.operator.enabled"
	labelEngine     = "swarmex.operator.engine"     // postgres, mysql
	labelPort       = "swarmex.operator.port"        // health check port
	labelBackupCron = "swarmex.operator.backup-cron" // cron expression for backups
	labelMinReady   = "swarmex.operator.min-ready"   // minimum healthy replicas

	defaultPort     = 5432
	defaultMinReady = 1
	reconcileInterval = 30 * time.Second
)

// DBConfig parsed from Docker service labels.
type DBConfig struct {
	Engine   string
	Port     int
	MinReady int
}

type dbState struct {
	config DBConfig
	name   string
}

// Operator manages database service lifecycle on Swarm.
type Operator struct {
	docker   *client.Client
	logger   *slog.Logger
	services map[string]*dbState
	mu       sync.Mutex
}

// New creates an Operator.
func New(cli *client.Client, logger *slog.Logger) *Operator {
	return &Operator{
		docker:   cli,
		logger:   logger,
		services: make(map[string]*dbState),
	}
}

// HandleEvent processes Docker service events.
func (o *Operator) HandleEvent(ctx context.Context, event events.Message) {
	if event.Type != events.ServiceEventType {
		return
	}
	switch event.Action {
	case events.ActionCreate, events.ActionUpdate:
		o.reconcileService(ctx, event.Actor.ID)
	case events.ActionRemove:
		o.mu.Lock()
		delete(o.services, event.Actor.ID)
		o.mu.Unlock()
	}
}

// RunReconcileLoop periodically checks all managed DB services.
func (o *Operator) RunReconcileLoop(ctx context.Context) {
	ticker := time.NewTicker(reconcileInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			o.reconcileAll(ctx)
		case <-ctx.Done():
			return
		}
	}
}

func (o *Operator) reconcileService(ctx context.Context, serviceID string) {
	svc, _, err := o.docker.ServiceInspectWithRaw(ctx, serviceID, types.ServiceInspectOptions{})
	if err != nil {
		return
	}
	labels := svc.Spec.Labels
	if labels[labelEnabled] != "true" {
		o.mu.Lock()
		delete(o.services, serviceID)
		o.mu.Unlock()
		return
	}

	cfg := parseDBConfig(labels)
	o.mu.Lock()
	o.services[serviceID] = &dbState{config: cfg, name: svc.Spec.Name}
	o.mu.Unlock()

	o.logger.Info("operator watching DB service", "service", svc.Spec.Name, "engine", cfg.Engine, "port", cfg.Port)
}

func (o *Operator) reconcileAll(ctx context.Context) {
	o.mu.Lock()
	snapshot := make(map[string]*dbState, len(o.services))
	for k, v := range o.services {
		snapshot[k] = v
	}
	o.mu.Unlock()

	for serviceID, state := range snapshot {
		o.checkHealth(ctx, serviceID, state)
	}
}

func (o *Operator) checkHealth(ctx context.Context, serviceID string, state *dbState) {
	f := filters.NewArgs()
	f.Add("service", serviceID)
	tasks, err := o.docker.TaskList(ctx, types.TaskListOptions{Filters: f})
	if err != nil {
		return
	}

	healthy := 0
	for _, task := range tasks {
		if task.Status.State != swarm.TaskStateRunning {
			continue
		}
		for _, att := range task.NetworksAttachments {
			for _, addr := range att.Addresses {
				ip := stripCIDR(addr)
				if tcpCheck(ip, state.config.Port) {
					healthy++
				}
			}
		}
	}

	if healthy < state.config.MinReady {
		o.logger.Error("DB service unhealthy",
			"service", state.name, "healthy", healthy, "min_ready", state.config.MinReady)
		o.triggerFailover(ctx, serviceID, state)
	}
}

func (o *Operator) triggerFailover(ctx context.Context, serviceID string, state *dbState) {
	// Force update to reschedule tasks
	svc, _, err := o.docker.ServiceInspectWithRaw(ctx, serviceID, types.ServiceInspectOptions{})
	if err != nil {
		return
	}
	svc.Spec.TaskTemplate.ForceUpdate++
	_, err = o.docker.ServiceUpdate(ctx, serviceID, svc.Version, svc.Spec, types.ServiceUpdateOptions{})
	if err != nil {
		o.logger.Error("failover force-update failed", "service", state.name, "error", err)
		return
	}
	o.logger.Warn("failover triggered", "service", state.name, "engine", state.config.Engine)
}

// ManagedServices returns count of managed DB services.
func (o *Operator) ManagedServices() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return len(o.services)
}

func tcpCheck(host string, port int) bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", host, port), 3*time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func stripCIDR(addr string) string {
	for i, c := range addr {
		if c == '/' {
			return addr[:i]
		}
	}
	return addr
}

func parseDBConfig(labels map[string]string) DBConfig {
	cfg := DBConfig{
		Engine:   "postgres",
		Port:     defaultPort,
		MinReady: defaultMinReady,
	}
	if v, ok := labels[labelEngine]; ok {
		cfg.Engine = v
		if v == "mysql" {
			cfg.Port = 3306
		}
	}
	if n, err := strconv.Atoi(labels[labelPort]); err == nil {
		cfg.Port = n
	}
	if n, err := strconv.Atoi(labels[labelMinReady]); err == nil {
		cfg.MinReady = n
	}
	return cfg
}
