package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/nornir-automation/gornir/pkg/gornir"
	"github.com/nornir-automation/gornir/pkg/plugins/connection"
	"github.com/nornir-automation/gornir/pkg/plugins/inventory"
	"github.com/nornir-automation/gornir/pkg/plugins/logger"
	"github.com/nornir-automation/gornir/pkg/plugins/output"
	"github.com/nornir-automation/gornir/pkg/plugins/runner"
	"github.com/nornir-automation/gornir/pkg/plugins/task"
	"gopkg.in/yaml.v3"
)

// InventoryData 定义了合并后的 hosts.yaml 的结构
type InventoryData struct {
	PlatformDefaults map[string][]string `yaml:"platform_defaults"`
	Nodes            map[string]HostData   `yaml:"nodes"`
}

// HostData 定义了 nodes 下每个主机的结构
type HostData struct {
	Hostname string            `yaml:"hostname"`
	Port     int               `yaml:"port"`
	Username string            `yaml:"username"`
	Password string            `yaml:"password"`
	Platform string            `yaml:"platform"`
	Commands []string          `yaml:"commands"` // 特定于主机的命令
	Data     map[string]interface{} `yaml:",inline"` // 允许其他自定义数据
}

func main() {
	log := logger.NewLogrus(false)

	inventoryFile := flag.String("i", "hosts.yaml", "Path to the inventory YAML file")
	overwriteFlag := flag.Bool("ow", false, "Overwrite platform default commands with host-specific commands")
	flag.BoolVar(overwriteFlag, "overwrite", false, "Overwrite platform default commands with host-specific commands (long version)")
	flag.Parse()

	plugin := inventory.FromFile(inventory.YAML, *inventoryFile)
	inv, err := plugin.Create()
	if err != nil {
		log.Fatal(err)
	}

	// 读取包含命令的完整 inventory 文件
	data, err := os.ReadFile(*inventoryFile)
	if err != nil {
		log.Fatalf("Failed to read inventory file: %v", err)
	}

	var inventoryData InventoryData
	err = yaml.Unmarshal(data, &inventoryData)
	if err != nil {
		log.Fatalf("Failed to unmarshal inventory data: %v", err)
	}

	gr := gornir.New().WithInventory(inv).WithLogger(log).WithRunner(runner.Parallel())

	// Open an SSH connection towards the devices
	results, err := gr.RunSync(
		context.Background(),
		&connection.SSHOpen{},
	)
	if err != nil {
		log.Fatal(err)
	}

	// defer closing the SSH connection we just opened
	defer func() {
		results, err = gr.RunSync(
			context.Background(),
			&connection.SSHClose{},
		)
		if err != nil {
			log.Fatal(err)
		}
	}()

	// 遍历主机并执行命令
	for name, host := range inventoryData.Nodes {
		var commandsToExecute []string

		// 获取平台默认命令
		if defaultCommands, ok := inventoryData.PlatformDefaults[host.Platform]; ok {
			commandsToExecute = append(commandsToExecute, defaultCommands...)
		}

		// 处理主机特定的命令
		if host.Commands != nil {
			if *overwriteFlag {
				// 覆盖平台默认命令
				commandsToExecute = host.Commands
			} else {
				// 追加主机特定命令
				commandsToExecute = append(commandsToExecute, host.Commands...)
			}
		}

		if len(commandsToExecute) > 0 {
			for _, cmd := range commandsToExecute {
				results, err = gr.RunSync(
					context.Background(),
					&task.RunCommand{Command: cmd}, // 使用 RunCommand
					gornir.WithHosts(name),         // 指定在哪个主机上运行
				)
				if err != nil {
					log.Fatalf("Error executing command '%s' on host '%s': %v", cmd, name, err)
				}
				output.RenderResults(os.Stdout, results, fmt.Sprintf("Output of '%s' on %s", cmd, name), true)
			}
		} else {
			log.Printf("No commands to execute for host '%s'.", name)
		}
	}
}
