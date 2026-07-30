package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"tokhub/internal/browserconnector"
	"tokhub/internal/buildinfo"
	"tokhub/internal/opencliconnector"
)

func main() {
	logger := log.New(os.Stdout, "TokHub 本地连接器: ", 0)
	if err := run(os.Args[1:], logger); err != nil {
		logger.Printf("%v", err)
		os.Exit(1)
	}
}

func run(args []string, logger *log.Logger) error {
	if len(args) == 0 {
		printUsage(logger)
		return errors.New("请选择一个命令")
	}
	if args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		printUsage(logger)
		return nil
	}
	if args[0] == "version" || args[0] == "--version" {
		logger.Printf("TokHub OpenCLI Connector %s", buildinfo.Version)
		return nil
	}
	configPath, err := opencliconnector.DefaultConfigPath()
	if err != nil {
		return err
	}
	switch args[0] {
	case "pair":
		flags := flag.NewFlagSet("pair", flag.ContinueOnError)
		serverURL := flags.String("server", "", "TokHub 服务地址")
		pairingCode := flags.String("code", "", "一次性配对码")
		flags.StringVar(&configPath, "config", configPath, "配置文件路径")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if strings.TrimSpace(*serverURL) == "" || strings.TrimSpace(*pairingCode) == "" {
			return errors.New("pair 需要 --server 和 --code")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		version, err := (opencliconnector.Executor{}).Version(ctx)
		if err != nil {
			return err
		}
		cfg, err := (opencliconnector.Client{}).Pair(ctx, *serverURL, *pairingCode)
		if err != nil {
			return fmt.Errorf("配对失败: %w", err)
		}
		if err := opencliconnector.SaveConfig(configPath, cfg); err != nil {
			return fmt.Errorf("保存本地连接配置: %w", err)
		}
		logger.Printf("配对成功（OpenCLI %s）。请运行 tokhub-opencli-connector run", version)
		return nil
	case "doctor":
		flags := flag.NewFlagSet("doctor", flag.ContinueOnError)
		flags.StringVar(&configPath, "config", configPath, "配置文件路径")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		version, err := (opencliconnector.Executor{}).Version(ctx)
		if err != nil {
			return err
		}
		if err := (opencliconnector.Executor{}).Doctor(ctx); err != nil {
			return err
		}
		cfg, err := opencliconnector.LoadConfig(configPath)
		if err != nil {
			return fmt.Errorf("本地连接配置不可用: %w", err)
		}
		if err := (opencliconnector.Client{Config: cfg}).Heartbeat(ctx, version); err != nil {
			return fmt.Errorf("TokHub 连接失败: %w", err)
		}
		logger.Printf("检查通过（OpenCLI %s，TokHub 已连接）", version)
		return nil
	case "run":
		flags := flag.NewFlagSet("run", flag.ContinueOnError)
		flags.StringVar(&configPath, "config", configPath, "配置文件路径")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		cfg, err := opencliconnector.LoadConfig(configPath)
		if err != nil {
			return fmt.Errorf("请先完成配对: %w", err)
		}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		return runConnector(ctx, cfg, logger)
	default:
		return fmt.Errorf("未知命令 %q；请使用 --help 查看说明", args[0])
	}
}

func printUsage(logger *log.Logger) {
	logger.Print(`用法:
  tokhub-opencli-connector pair --server <TokHub 地址> --code <一次性配对码>
  tokhub-opencli-connector run
  tokhub-opencli-connector doctor
  tokhub-opencli-connector version

说明:
  pair    将当前电脑与个人 TokHub 工作区配对
  run     保持本机连接，执行受限的 ChatGPT、Gemini、DeepSeek 网页任务
  doctor  检查 OpenCLI 版本和 TokHub 连接
  version 显示本地连接器版本`)
}

func runConnector(ctx context.Context, cfg opencliconnector.Config, logger *log.Logger) error {
	executor := opencliconnector.Executor{}
	version, err := executor.Version(ctx)
	if err != nil {
		return err
	}
	doctorCtx, cancelDoctor := context.WithTimeout(ctx, 30*time.Second)
	if err := heartbeatIfBrowserHealthy(doctorCtx, opencliconnector.Client{Config: cfg}, executor, version); err != nil {
		cancelDoctor()
		return fmt.Errorf("首次连接检查失败: %w", err)
	}
	cancelDoctor()
	client := opencliconnector.Client{Config: cfg}
	logger.Printf("已连接（OpenCLI %s）。保持 Chrome 和 OpenCLI 扩展运行即可", version)
	var browserHealthy atomic.Bool
	browserHealthy.Store(true)
	var serverHealthy atomic.Bool
	serverHealthy.Store(true)
	go maintainServerHeartbeat(ctx, client, version, 15*time.Second, &browserHealthy, &serverHealthy, logger)
	browserCheck := time.NewTicker(15 * time.Second)
	defer browserCheck.Stop()
	poll := time.NewTicker(time.Second)
	defer poll.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-browserCheck.C:
			checkCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := executor.Doctor(checkCtx)
			cancel()
			browserHealthy.Store(err == nil)
			if err != nil {
				logger.Printf("Chrome Bridge 暂时中断，将自动重试")
			}
		case <-poll.C:
			if !browserHealthy.Load() || !serverHealthy.Load() {
				continue
			}
			if err := processOneTask(ctx, client, executor); err != nil {
				logger.Printf("任务处理失败: %v", err)
			}
		}
	}
}

func maintainServerHeartbeat(
	ctx context.Context,
	client opencliconnector.Client,
	version string,
	interval time.Duration,
	browserHealthy *atomic.Bool,
	healthy *atomic.Bool,
	logger *log.Logger,
) {
	if interval <= 0 {
		interval = 15 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if browserHealthy != nil && !browserHealthy.Load() {
				continue
			}
			heartbeatCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := client.Heartbeat(heartbeatCtx, version)
			cancel()
			if ctx.Err() != nil {
				return
			}
			healthy.Store(err == nil)
			if err != nil && logger != nil {
				logger.Printf("TokHub 连接暂时中断，将自动重试")
			}
		}
	}
}

func heartbeatIfBrowserHealthy(
	ctx context.Context,
	client opencliconnector.Client,
	executor opencliconnector.Executor,
	version string,
) error {
	if err := executor.Doctor(ctx); err != nil {
		return err
	}
	return client.Heartbeat(ctx, version)
}

func processOneTask(ctx context.Context, client opencliconnector.Client, executor opencliconnector.Executor) error {
	claimCtx, cancelClaim := context.WithTimeout(ctx, 10*time.Second)
	task, err := client.Claim(claimCtx)
	cancelClaim()
	if err != nil || task == nil {
		return err
	}
	prompt, _ := task.Request["prompt"].(string)
	taskTimeout := 100 * time.Second
	if task.Action == browserconnector.ActionStatus {
		taskTimeout = 35 * time.Second
	}
	taskCtx, cancelTask := context.WithTimeout(ctx, taskTimeout)
	var result browserconnector.Result
	if task.Action == browserconnector.ActionAsk {
		status := executor.Execute(taskCtx, browserconnector.Task{
			Provider: task.Provider,
			Action:   browserconnector.ActionStatus,
		})
		if status.OK {
			status.AccountFingerprint = browserconnector.BindAccountFingerprint(
				client.Config.DeviceToken,
				status.AccountFingerprint,
			)
		}
		expectedFingerprint, _ := task.Request["accountFingerprint"].(string)
		switch {
		case !status.OK:
			result = status
		case !browserconnector.AccountFingerprintMatches(expectedFingerprint, status.AccountFingerprint):
			result = browserconnector.Result{
				OK: false, ErrorCode: "identity_mismatch",
				ErrorMessage: "当前网页登录账号与连接创建时不一致，请切换回原账号或重新建立连接",
			}
		default:
			result = executor.Execute(taskCtx, browserconnector.Task{
				Provider: task.Provider,
				Action:   task.Action,
				Prompt:   prompt,
			})
		}
	} else {
		result = executor.Execute(taskCtx, browserconnector.Task{
			Provider: task.Provider,
			Action:   task.Action,
			Prompt:   prompt,
		})
		if task.Action == browserconnector.ActionStatus && result.OK {
			result.AccountFingerprint = browserconnector.BindAccountFingerprint(
				client.Config.DeviceToken,
				result.AccountFingerprint,
			)
		}
	}
	cancelTask()
	completeCtx, cancelComplete := context.WithTimeout(ctx, 10*time.Second)
	defer cancelComplete()
	return client.Complete(completeCtx, task.ID, task.LeaseToken, result)
}
