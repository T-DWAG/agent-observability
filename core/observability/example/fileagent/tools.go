package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
)

const maxReadBytes = 8 << 10 // 8KiB，防止一次读爆

type listInput struct{}

type listOutput struct {
	Files []string `json:"files"`
}

type readInput struct {
	Path string `json:"path" jsonschema:"description=相对 sandbox 的文件名，如 obs_hints.md,required=true"`
}

type readOutput struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func sandboxRoot() (string, error) {
	// 以可执行/测试时的工作目录为准：请在 module 根或 example/fileagent 下运行
	// 候选相对路径：程序可能从不同工作目录启动，所以准备两处常见位置
	// 1) 从 module 根目录跑：example/fileagent/sandbox
	// 2) 从 example/fileagent 目录跑：sandbox
	// 形成的是 example/fileagent/sandbox，不是 example/fileagent/sandbox/sandbox
	candidates := []string{
		filepath.Join("example", "fileagent", "sandbox"),
		"sandbox",
	}
	// 按顺序探测：哪个路径真实存在且是目录，就用哪个
	for _, c := range candidates {
		st, err := os.Stat(c) // 查看路径是否存在、是文件还是目录
		if err == nil && st.IsDir() {
			// 转成绝对路径，后续读文件时路径更稳定、不易歧义
			abs, err := filepath.Abs(c)
			if err != nil {
				return "", err
			}
			return abs, nil
		}
	}
	// 两个候选都找不到：提示调用方从正确目录启动
	return "", fmt.Errorf("sandbox dir not found (run from module root or example/fileagent)")
}

// resolveSandboxFile：精确名优先；无扩展名且找不到时再试 .md（模型常漏写扩展名）。
func resolveSandboxFile(root, rel string) (resolvedRel, full string, err error) {
	full, err = safeJoin(root, rel)
	if err != nil {
		return "", "", err
	}
	if _, err := os.Stat(full); err == nil {
		return filepath.Clean(rel), full, nil
	}
	if filepath.Ext(rel) == "" {
		try := rel + ".md"
		full2, err2 := safeJoin(root, try)
		if err2 == nil {
			if _, err3 := os.Stat(full2); err3 == nil {
				return try, full2, nil
			}
		}
	}
	return "", "", fmt.Errorf("file not found: %q (use exact name from list_files, e.g. README.md)", rel)
}

// safeJoin 拼接路径，确保不超出 sandbox 目录
func safeJoin(root, rel string) (string, error) {
	// filepath.Clean：规范化相对路径，去掉多余分隔符、"."、以及可折叠的".."
	// 例如 "a/./b/../c" → "a/c"；空串会变成 "."
	// 先 Clean 再校验，避免用未规范化路径绕过后面的沙箱检查
	rel = filepath.Clean(rel)
	if rel == "." || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return "", fmt.Errorf("invalid path %q", rel)
	}
	full := filepath.Join(root, rel)
	absRoot, err := filepath.Abs(root) // 转成绝对路径，后续比较更稳定、不易歧义
	if err != nil {
		return "", err
	}
	absFull, err := filepath.Abs(full) // 转成绝对路径，后续比较更稳定、不易歧义
	if err != nil {
		return "", err
	}
	sep := string(os.PathSeparator) // 路径分隔符，Windows 是 '\'，Linux 是 '/'
	if absFull != absRoot && !strings.HasPrefix(absFull, absRoot+sep) {
		// 如果拼接后的路径不是 sandbox 的子路径，说明试图逃逸沙箱
		return "", fmt.Errorf("path escapes sandbox: %q", rel)
	}
	return absFull, nil
}

func newFileTools() ([]tool.BaseTool, error) {
	// 创建两个工具：列出文件、读取文本
	listTool, err := utils.InferTool(
		"list_files",
		"列出 sandbox 目录下的文件名。回答「有哪些文件」前应先调用本工具。",
		func(_ context.Context, _ listInput) (listOutput, error) {
			root, err := sandboxRoot()
			if err != nil {
				return listOutput{}, err
			}
			entries, err := os.ReadDir(root)
			if err != nil {
				return listOutput{}, err
			}
			out := listOutput{Files: make([]string, 0, len(entries))}
			for _, e := range entries {
				if !e.IsDir() {
					out.Files = append(out.Files, e.Name())
				}
			}
			return out, nil
		},
	)
	if err != nil {
		return nil, err
	}

	// 创建读取文本工具
	readTool, err := utils.InferTool(
		"read_file",
		"读取 sandbox 内某个文本文件。path 须为 list_files 返回的完整文件名（含扩展名，如 README.md）。",
		func(_ context.Context, in readInput) (readOutput, error) {
			root, err := sandboxRoot()
			if err != nil {
				return readOutput{}, err
			}
			rel, full, err := resolveSandboxFile(root, in.Path)
			if err != nil {
				return readOutput{}, err
			}
			data, err := os.ReadFile(full)
			if err != nil {
				return readOutput{}, err
			}
			if len(data) > maxReadBytes {
				data = data[:maxReadBytes]
			}
			return readOutput{Path: rel, Content: string(data)}, nil
		},
	)
	if err != nil { // 创建工具失败，返回错误
		return nil, err
	}

	return []tool.BaseTool{listTool, readTool}, nil // 返回两个工具
}
