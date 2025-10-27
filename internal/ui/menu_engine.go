package ui

import (
	"fmt"
	"strconv"
	"strings"
	"pixly/internal/output"
)

// MenuItem 菜单项 - 简单数据结构，无特殊情况
type MenuItem struct {
	ID          string
	Title       string
	Description string
	Action      func() error
}

// Menu 菜单 - 统一的菜单结构
type Menu struct {
	Title string
	Items []MenuItem
}

// 简化的菜单显示 - 直接使用OutputController
func DisplayMenuSimple(menu *Menu) error {
	oc := output.GetOutputController()

	// 清屏
	oc.Clear()

	// 显示标题
	var titleBuilder strings.Builder
	titleBuilder.WriteString("\n=== ")
	titleBuilder.WriteString(menu.Title)
	titleBuilder.WriteString(" ===")
	oc.WriteColorLine(titleBuilder.String(), getBrandColor())
	oc.WriteLine("")

	// 显示菜单项
	for i, item := range menu.Items {
		var itemBuilder strings.Builder
		itemBuilder.WriteString(strconv.Itoa(i + 1))
		itemBuilder.WriteString(". ")
		itemBuilder.WriteString(item.Title)
		oc.WriteColorLine(itemBuilder.String(), getAccentColor())
		if item.Description != "" {
			var descBuilder strings.Builder
			descBuilder.WriteString("   ")
			descBuilder.WriteString(item.Description)
			oc.WriteColorLine(descBuilder.String(), getInfoColor())
		}
	}

	oc.WriteLine("")
	oc.Flush()
	return nil
}

// 简化的用户选择 - 直接处理
func GetMenuChoiceSimple(menu *Menu) (*MenuItem, error) {
	for {
		// 显示菜单
		err := DisplayMenuSimple(menu)
		if err != nil {
			return nil, err
		}

		// 获取用户输入
		input := PromptUser("请选择")

		// 解析选择
		item := parseChoice(menu, input)
		if item != nil {
			return item, nil
		}

		// 无效选择，显示错误
		if err := RenderError("无效选择，请重试"); err != nil {
			return nil, fmt.Errorf("failed to render error: %w", err)
		}
	}
}

// parseChoice 解析用户选择
func parseChoice(menu *Menu, input string) *MenuItem {
	input = strings.TrimSpace(input)

	// 尝试按ID匹配
	for i := range menu.Items {
		if menu.Items[i].ID == input {
			return &menu.Items[i]
		}
	}

	// 尝试按数字索引匹配
	if index, err := strconv.Atoi(input); err == nil {
		if index >= 1 && index <= len(menu.Items) {
			return &menu.Items[index-1]
		}
	}

	return nil
}

// 保持向后兼容的函数
func DisplayMenuNew(menu *Menu) error {
	return DisplayMenuSimple(menu)
}

func GetMenuChoiceNew(menu *Menu) (*MenuItem, error) {
	return GetMenuChoiceSimple(menu)
}

// CreateSimpleMenu 创建简单菜单
func CreateSimpleMenu(title string, items map[string]string, actions map[string]func() error) *Menu {
	menu := &Menu{
		Title: title,
		Items: make([]MenuItem, 0, len(items)),
	}

	for id, title := range items {
		item := MenuItem{
			ID:    id,
			Title: title,
		}

		if action, exists := actions[id]; exists {
			item.Action = action
		}

		menu.Items = append(menu.Items, item)
	}

	return menu
}
