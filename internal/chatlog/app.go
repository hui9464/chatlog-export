package chatlog

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/sjzar/chatlog/internal/chatlog/ctx"
	"github.com/sjzar/chatlog/internal/export"
	"github.com/sjzar/chatlog/internal/ui/footer"
	"github.com/sjzar/chatlog/internal/ui/form"
	"github.com/sjzar/chatlog/internal/ui/help"
	"github.com/sjzar/chatlog/internal/ui/infobar"
	"github.com/sjzar/chatlog/internal/ui/menu"
	"github.com/sjzar/chatlog/internal/wechat"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

const (
	RefreshInterval = 1000 * time.Millisecond
)

type App struct {
	*tview.Application

	ctx         *ctx.Context
	m           *Manager
	stopRefresh chan struct{}

	// page
	mainPages *tview.Pages
	infoBar   *infobar.InfoBar
	tabPages  *tview.Pages
	footer    *footer.Footer

	// tab
	menu      *menu.Menu
	help      *help.Help
	activeTab int
	tabCount  int
}

func NewApp(ctx *ctx.Context, m *Manager) *App {
	app := &App{
		ctx:         ctx,
		m:           m,
		Application: tview.NewApplication(),
		mainPages:   tview.NewPages(),
		infoBar:     infobar.New(),
		tabPages:    tview.NewPages(),
		footer:      footer.New(),
		menu:        menu.New("主菜单"),
		help:        help.New(),
	}

	app.initMenu()

	app.updateMenuItemsState()

	return app
}

func (a *App) Run() error {

	flex := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(a.infoBar, infobar.InfoBarViewHeight, 0, false).
		AddItem(a.tabPages, 0, 1, true).
		AddItem(a.footer, 1, 1, false)

	a.mainPages.AddPage("main", flex, true, true)

	a.tabPages.
		AddPage("0", a.menu, true, true).
		AddPage("1", a.help, true, false)
	a.tabCount = 2

	a.SetInputCapture(a.inputCapture)

	go a.refresh()

	if err := a.SetRoot(a.mainPages, true).EnableMouse(false).Run(); err != nil {
		return err
	}

	return nil
}

func (a *App) Stop() {
	// 添加一个通道用于停止刷新 goroutine
	if a.stopRefresh != nil {
		close(a.stopRefresh)
	}
	a.Application.Stop()
}

func (a *App) updateMenuItemsState() {
	// 查找并更新自动解密菜单项
	for _, item := range a.menu.GetItems() {
		// 更新自动解密菜单项
		if item.Index == 5 {
			if a.ctx.AutoDecrypt {
				item.Name = "停止自动解密"
				item.Description = "停止监控数据目录更新，不再自动解密新增数据"
			} else {
				item.Name = "开启自动解密"
				item.Description = "监控数据目录更新，自动解密新增数据"
			}
		}

		// 更新HTTP服务菜单项
		if item.Index == 4 {
			if a.ctx.HTTPEnabled {
				item.Name = "停止 HTTP 服务"
				item.Description = "停止本地 HTTP & MCP 服务器"
			} else {
				item.Name = "启动 HTTP 服务"
				item.Description = "启动本地 HTTP & MCP 服务器"
			}
		}
	}
}

func (a *App) switchTab(step int) {
	index := (a.activeTab + step) % a.tabCount
	if index < 0 {
		index = a.tabCount - 1
	}
	a.activeTab = index
	a.tabPages.SwitchToPage(fmt.Sprint(a.activeTab))
}

func (a *App) refresh() {
	tick := time.NewTicker(RefreshInterval)
	defer tick.Stop()

	for {
		select {
		case <-a.stopRefresh:
			return
		case <-tick.C:
			if a.ctx.AutoDecrypt || a.ctx.HTTPEnabled {
				a.m.RefreshSession()
			}
			a.infoBar.UpdateAccount(a.ctx.Account)
			a.infoBar.UpdateBasicInfo(a.ctx.PID, a.ctx.FullVersion, a.ctx.ExePath)
			a.infoBar.UpdateStatus(a.ctx.Status)
			a.infoBar.UpdateDataKey(a.ctx.DataKey)
			a.infoBar.UpdatePlatform(a.ctx.Platform)
			a.infoBar.UpdateDataUsageDir(a.ctx.DataUsage, a.ctx.DataDir)
			a.infoBar.UpdateWorkUsageDir(a.ctx.WorkUsage, a.ctx.WorkDir)
			if a.ctx.LastSession.Unix() > 1000000000 {
				a.infoBar.UpdateSession(a.ctx.LastSession.Format("2006-01-02 15:04:05"))
			}
			if a.ctx.HTTPEnabled {
				a.infoBar.UpdateHTTPServer(fmt.Sprintf("[green][已启动][white] [%s]", a.ctx.HTTPAddr))
			} else {
				a.infoBar.UpdateHTTPServer("[未启动]")
			}
			if a.ctx.AutoDecrypt {
				a.infoBar.UpdateAutoDecrypt("[green][已开启][white]")
			} else {
				a.infoBar.UpdateAutoDecrypt("[未开启]")
			}

			a.Draw()
		}
	}
}

func (a *App) inputCapture(event *tcell.EventKey) *tcell.EventKey {

	// 如果当前页面不是主页面，ESC 键返回主页面
	if a.mainPages.HasPage("submenu") && event.Key() == tcell.KeyEscape {
		a.mainPages.RemovePage("submenu")
		a.mainPages.SwitchToPage("main")
		return nil
	}

	if a.tabPages.HasFocus() {
		switch event.Key() {
		case tcell.KeyLeft:
			a.switchTab(-1)
			return nil
		case tcell.KeyRight:
			a.switchTab(1)
			return nil
		}
	}

	switch event.Key() {
	case tcell.KeyCtrlC:
		a.Stop()
	}

	return event
}

func (a *App) initMenu() {
	getDataKey := &menu.Item{
		Index:       2,
		Name:        "获取数据密钥",
		Description: "从进程获取数据密钥",
		Selected: func(i *menu.Item) {
			modal := tview.NewModal()
			if runtime.GOOS == "darwin" {
				modal.SetText("获取数据密钥中...\n预计需要 20 秒左右的时间，期间微信会卡住，请耐心等待")
			} else {
				modal.SetText("获取数据密钥中...")
			}
			a.mainPages.AddPage("modal", modal, true, true)
			a.SetFocus(modal)

			go func() {
				err := a.m.GetDataKey()

				// 在主线程中更新UI
				a.QueueUpdateDraw(func() {
					if err != nil {
						// 解密失败
						modal.SetText("获取数据密钥失败: " + err.Error())
					} else {
						// 解密成功
						modal.SetText("获取数据密钥成功")
					}

					// 添加确认按钮
					modal.AddButtons([]string{"OK"})
					modal.SetDoneFunc(func(buttonIndex int, buttonLabel string) {
						a.mainPages.RemovePage("modal")
					})
					a.SetFocus(modal)
				})
			}()
		},
	}

	decryptData := &menu.Item{
		Index:       3,
		Name:        "解密数据",
		Description: "解密数据文件",
		Selected: func(i *menu.Item) {
			// 创建一个没有按钮的模态框，显示"解密中..."
			modal := tview.NewModal().
				SetText("解密中...")

			a.mainPages.AddPage("modal", modal, true, true)
			a.SetFocus(modal)

			// 在后台执行解密操作
			go func() {
				// 执行解密
				err := a.m.DecryptDBFiles()

				// 在主线程中更新UI
				a.QueueUpdateDraw(func() {
					if err != nil {
						// 解密失败
						modal.SetText("解密失败: " + err.Error())
					} else {
						// 解密成功
						modal.SetText("解密数据成功")
					}

					// 添加确认按钮
					modal.AddButtons([]string{"OK"})
					modal.SetDoneFunc(func(buttonIndex int, buttonLabel string) {
						a.mainPages.RemovePage("modal")
					})
					a.SetFocus(modal)
				})
			}()
		},
	}

	httpServer := &menu.Item{
		Index:       4,
		Name:        "启动 HTTP 服务",
		Description: "启动本地 HTTP & MCP 服务器",
		Selected: func(i *menu.Item) {
			modal := tview.NewModal()

			// 根据当前服务状态执行不同操作
			if !a.ctx.HTTPEnabled {
				// HTTP 服务未启动，启动服务
				modal.SetText("正在启动 HTTP 服务...")
				a.mainPages.AddPage("modal", modal, true, true)
				a.SetFocus(modal)

				// 在后台启动服务
				go func() {
					err := a.m.StartService()

					// 在主线程中更新UI
					a.QueueUpdateDraw(func() {
						if err != nil {
							// 启动失败
							modal.SetText("启动 HTTP 服务失败: " + err.Error())
						} else {
							// 启动成功
							modal.SetText("已启动 HTTP 服务")
						}

						// 更改菜单项名称
						a.updateMenuItemsState()

						// 添加确认按钮
						modal.AddButtons([]string{"OK"})
						modal.SetDoneFunc(func(buttonIndex int, buttonLabel string) {
							a.mainPages.RemovePage("modal")
						})
						a.SetFocus(modal)
					})
				}()
			} else {
				// HTTP 服务已启动，停止服务
				modal.SetText("正在停止 HTTP 服务...")
				a.mainPages.AddPage("modal", modal, true, true)
				a.SetFocus(modal)

				// 在后台停止服务
				go func() {
					err := a.m.StopService()

					// 在主线程中更新UI
					a.QueueUpdateDraw(func() {
						if err != nil {
							// 停止失败
							modal.SetText("停止 HTTP 服务失败: " + err.Error())
						} else {
							// 停止成功
							modal.SetText("已停止 HTTP 服务")
						}

						// 更改菜单项名称
						a.updateMenuItemsState()

						// 添加确认按钮
						modal.AddButtons([]string{"OK"})
						modal.SetDoneFunc(func(buttonIndex int, buttonLabel string) {
							a.mainPages.RemovePage("modal")
						})
						a.SetFocus(modal)
					})
				}()
			}
		},
	}

	autoDecrypt := &menu.Item{
		Index:       5,
		Name:        "开启自动解密",
		Description: "自动解密新增的数据文件",
		Selected: func(i *menu.Item) {
			modal := tview.NewModal()

			// 根据当前自动解密状态执行不同操作
			if !a.ctx.AutoDecrypt {
				// 自动解密未开启，开启自动解密
				modal.SetText("正在开启自动解密...")
				a.mainPages.AddPage("modal", modal, true, true)
				a.SetFocus(modal)

				// 在后台开启自动解密
				go func() {
					err := a.m.StartAutoDecrypt()

					// 在主线程中更新UI
					a.QueueUpdateDraw(func() {
						if err != nil {
							// 开启失败
							modal.SetText("开启自动解密失败: " + err.Error())
						} else {
							// 开启成功
							if a.ctx.Version == 3 {
								modal.SetText("已开启自动解密\n3.x版本数据文件更新不及时，有低延迟需求请使用4.0版本")
							} else {
								modal.SetText("已开启自动解密")
							}
						}

						// 更改菜单项名称
						a.updateMenuItemsState()

						// 添加确认按钮
						modal.AddButtons([]string{"OK"})
						modal.SetDoneFunc(func(buttonIndex int, buttonLabel string) {
							a.mainPages.RemovePage("modal")
						})
						a.SetFocus(modal)
					})
				}()
			} else {
				// 自动解密已开启，停止自动解密
				modal.SetText("正在停止自动解密...")
				a.mainPages.AddPage("modal", modal, true, true)
				a.SetFocus(modal)

				// 在后台停止自动解密
				go func() {
					err := a.m.StopAutoDecrypt()

					// 在主线程中更新UI
					a.QueueUpdateDraw(func() {
						if err != nil {
							// 停止失败
							modal.SetText("停止自动解密失败: " + err.Error())
						} else {
							// 停止成功
							modal.SetText("已停止自动解密")
						}

						// 更改菜单项名称
						a.updateMenuItemsState()

						// 添加确认按钮
						modal.AddButtons([]string{"OK"})
						modal.SetDoneFunc(func(buttonIndex int, buttonLabel string) {
							a.mainPages.RemovePage("modal")
						})
						a.SetFocus(modal)
					})
				}()
			}
		},
	}

	setting := &menu.Item{
		Index:       6,
		Name:        "设置",
		Description: "设置应用程序选项",
		Selected:    a.settingSelected,
	}

	export := &menu.Item{
		Index:       7,
		Name:        "导出聊天记录",
		Description: "导出聊天记录到文件",
		Selected: func(i *menu.Item) {
			// 创建一个子菜单
			subMenu := menu.NewSubMenu("导出聊天记录")

			// 添加导出选项
			subMenu.AddItem(&menu.Item{
				Index:       1,
				Name:        "导出为 JSON",
				Description: "将聊天记录导出为 JSON 格式",
				Selected: func(i *menu.Item) {
					// 显示导出中的模态框
					modal := tview.NewModal().SetText("正在导出聊天记录...")
					a.mainPages.AddPage("modal", modal, true, true)
					a.SetFocus(modal)

					// 在后台执行导出操作
					go func() {
						// 获取所有消息
						messages, err := export.GetMessagesForExport(a.m.db, time.Time{}, time.Time{}, "", false, func(current, total int, msg any) {
							percentage := float64(current) / float64(total) * 100
							width := 20 // 进度条宽度
							completed := int(float64(width) * float64(current) / float64(total))
							remaining := width - completed

							// 构建进度条
							progressBar := fmt.Sprintf("正在导出聊天记录\n\n正在获取消息列表...\n[%s%s] %.1f%%\n(%d/%d)",
								strings.Repeat("█", completed),
								strings.Repeat("░", remaining),
								percentage,
								current,
								total)

							a.QueueUpdateDraw(func() {
								modal.SetText(progressBar)
							})
						})
						if err != nil {
							// 在主线程中更新UI
							a.QueueUpdateDraw(func() {
								modal.SetText("导出失败: " + err.Error())
								modal.AddButtons([]string{"OK"})
								modal.SetDoneFunc(func(buttonIndex int, buttonLabel string) {
									a.mainPages.RemovePage("modal")
								})
								a.SetFocus(modal)
							})
							return
						}

						// 导出为JSON
						outputPath := fmt.Sprintf("chatlog_%s.json", time.Now().Format("20060102_150405"))
						if err := export.ExportMessages(messages, outputPath, "json", func(current, total int) {
							percentage := float64(current) / float64(total) * 100
							width := 20 // 进度条宽度
							completed := int(float64(width) * float64(current) / float64(total))
							remaining := width - completed

							// 构建进度条
							progressBar := fmt.Sprintf("正在导出聊天记录\n\n正在写入文件...\n[%s%s] %.1f%%\n(%d/%d)",
								strings.Repeat("█", completed),
								strings.Repeat("░", remaining),
								percentage,
								current,
								total)

							a.QueueUpdateDraw(func() {
								modal.SetText(progressBar)
							})
						}); err != nil {
							// 在主线程中更新UI
							a.QueueUpdateDraw(func() {
								modal.SetText("导出失败: " + err.Error())
								modal.AddButtons([]string{"OK"})
								modal.SetDoneFunc(func(buttonIndex int, buttonLabel string) {
									a.mainPages.RemovePage("modal")
								})
								a.SetFocus(modal)
							})
							return
						}

						// 在主线程中更新UI
						a.QueueUpdateDraw(func() {
							modal.SetText(fmt.Sprintf("导出成功\n文件已保存到: %s", outputPath))
							modal.AddButtons([]string{"OK"})
							modal.SetDoneFunc(func(buttonIndex int, buttonLabel string) {
								a.mainPages.RemovePage("modal")
							})
							a.SetFocus(modal)
						})
					}()
				},
			})

			subMenu.AddItem(&menu.Item{
				Index:       2,
				Name:        "导出为 CSV",
				Description: "将聊天记录导出为 CSV 格式",
				Selected: func(i *menu.Item) {
					// 显示导出中的模态框
					modal := tview.NewModal().SetText("正在导出聊天记录...")
					a.mainPages.AddPage("modal", modal, true, true)
					a.SetFocus(modal)

					// 在后台执行导出操作
					go func() {
						// 获取所有消息
						messages, err := export.GetMessagesForExport(a.m.db, time.Time{}, time.Time{}, "", false, func(current, total int, msg any) {
							percentage := float64(current) / float64(total) * 100
							width := 20 // 进度条宽度
							completed := int(float64(width) * float64(current) / float64(total))
							remaining := width - completed

							// 构建进度条
							progressBar := fmt.Sprintf("正在导出聊天记录\n\n正在获取消息列表...\n[%s%s] %.1f%%\n(%d/%d)",
								strings.Repeat("█", completed),
								strings.Repeat("░", remaining),
								percentage,
								current,
								total)

							a.QueueUpdateDraw(func() {
								modal.SetText(progressBar)
							})
						})
						if err != nil {
							// 在主线程中更新UI
							a.QueueUpdateDraw(func() {
								modal.SetText("导出失败: " + err.Error())
								modal.AddButtons([]string{"OK"})
								modal.SetDoneFunc(func(buttonIndex int, buttonLabel string) {
									a.mainPages.RemovePage("modal")
								})
								a.SetFocus(modal)
							})
							return
						}

						// 导出为CSV
						outputPath := fmt.Sprintf("chatlog_%s.csv", time.Now().Format("20060102_150405"))
						if err := export.ExportMessages(messages, outputPath, "csv", func(current, total int) {
							percentage := float64(current) / float64(total) * 100
							width := 20 // 进度条宽度
							completed := int(float64(width) * float64(current) / float64(total))
							remaining := width - completed

							// 构建进度条
							progressBar := fmt.Sprintf("正在导出聊天记录\n\n[%s%s] %.1f%%\n(%d/%d)",
								strings.Repeat("█", completed),
								strings.Repeat("░", remaining),
								percentage,
								current,
								total)

							a.QueueUpdateDraw(func() {
								modal.SetText(progressBar)
							})
						}); err != nil {
							// 在主线程中更新UI
							a.QueueUpdateDraw(func() {
								modal.SetText("导出失败: " + err.Error())
								modal.AddButtons([]string{"OK"})
								modal.SetDoneFunc(func(buttonIndex int, buttonLabel string) {
									a.mainPages.RemovePage("modal")
								})
								a.SetFocus(modal)
							})
							return
						}

						// 在主线程中更新UI
						a.QueueUpdateDraw(func() {
							modal.SetText(fmt.Sprintf("导出成功\n文件已保存到: %s", outputPath))
							modal.AddButtons([]string{"OK"})
							modal.SetDoneFunc(func(buttonIndex int, buttonLabel string) {
								a.mainPages.RemovePage("modal")
							})
							a.SetFocus(modal)
						})
					}()
				},
			})

			subMenu.AddItem(&menu.Item{
				Index:       3,
				Name:        "导出我的发言",
				Description: "导出当前账号的所有发言记录",
				Selected: func(i *menu.Item) {
					// 创建格式选择子菜单
					formatMenu := menu.NewSubMenu("选择导出格式")

					// 添加 JSON 格式选项
					formatMenu.AddItem(&menu.Item{
						Index:       1,
						Name:        "导出为 JSON",
						Description: "将发言记录导出为 JSON 格式",
						Selected: func(i *menu.Item) {
							// 显示导出中的模态框
							modal := tview.NewModal().SetText("正在导出聊天记录...")
							a.mainPages.AddPage("modal", modal, true, true)
							a.SetFocus(modal)

							// 在后台执行导出操作
							go func() {
								// 获取所有消息
								messages, err := export.GetMessagesForExport(a.m.db, time.Time{}, time.Time{}, "", true, func(current, total int, msg any) {
									percentage := float64(current) / float64(total) * 100
									width := 20 // 进度条宽度
									completed := int(float64(width) * float64(current) / float64(total))
									remaining := width - completed

									// 构建进度条
									progressBar := fmt.Sprintf("正在导出聊天记录\n\n正在获取消息列表...\n[%s%s] %.1f%%\n(%d/%d)",
										strings.Repeat("█", completed),
										strings.Repeat("░", remaining),
										percentage,
										current,
										total)

									a.QueueUpdateDraw(func() {
										modal.SetText(progressBar)
									})
								})
								if err != nil {
									// 在主线程中更新UI
									a.QueueUpdateDraw(func() {
										modal.SetText("导出失败: " + err.Error())
										modal.AddButtons([]string{"OK"})
										modal.SetDoneFunc(func(buttonIndex int, buttonLabel string) {
											a.mainPages.RemovePage("modal")
										})
										a.SetFocus(modal)
									})
									return
								}

								// 导出为JSON
								outputPath := fmt.Sprintf("my_chatlog_%s.json", time.Now().Format("20060102_150405"))
								if err := export.ExportMessages(messages, outputPath, "json", func(current, total int) {
									percentage := float64(current) / float64(total) * 100
									width := 20 // 进度条宽度
									completed := int(float64(width) * float64(current) / float64(total))
									remaining := width - completed

									// 构建进度条
									progressBar := fmt.Sprintf("正在导出聊天记录\n\n[%s%s] %.1f%%\n(%d/%d)",
										strings.Repeat("█", completed),
										strings.Repeat("░", remaining),
										percentage,
										current,
										total)

									a.QueueUpdateDraw(func() {
										modal.SetText(progressBar)
									})
								}); err != nil {
									// 在主线程中更新UI
									a.QueueUpdateDraw(func() {
										modal.SetText("导出失败: " + err.Error())
										modal.AddButtons([]string{"OK"})
										modal.SetDoneFunc(func(buttonIndex int, buttonLabel string) {
											a.mainPages.RemovePage("modal")
										})
										a.SetFocus(modal)
									})
									return
								}

								// 在主线程中更新UI
								a.QueueUpdateDraw(func() {
									modal.SetText(fmt.Sprintf("导出成功\n文件已保存到: %s", outputPath))
									modal.AddButtons([]string{"OK"})
									modal.SetDoneFunc(func(buttonIndex int, buttonLabel string) {
										a.mainPages.RemovePage("modal")
									})
									a.SetFocus(modal)
								})
							}()
						},
					})

					// 添加 CSV 格式选项
					formatMenu.AddItem(&menu.Item{
						Index:       2,
						Name:        "导出为 CSV",
						Description: "将发言记录导出为 CSV 格式",
						Selected: func(i *menu.Item) {
							// 显示导出中的模态框
							modal := tview.NewModal().SetText("正在导出聊天记录...")
							a.mainPages.AddPage("modal", modal, true, true)
							a.SetFocus(modal)

							// 在后台执行导出操作
							go func() {
								// 获取所有消息
								messages, err := export.GetMessagesForExport(a.m.db, time.Time{}, time.Time{}, "", true, func(current, total int, msg any) {
									percentage := float64(current) / float64(total) * 100
									width := 20 // 进度条宽度
									completed := int(float64(width) * float64(current) / float64(total))
									remaining := width - completed

									// 构建进度条
									progressBar := fmt.Sprintf("正在导出聊天记录\n\n正在获取消息列表...\n[%s%s] %.1f%%\n(%d/%d)",
										strings.Repeat("█", completed),
										strings.Repeat("░", remaining),
										percentage,
										current,
										total)

									a.QueueUpdateDraw(func() {
										modal.SetText(progressBar)
									})
								})
								if err != nil {
									// 在主线程中更新UI
									a.QueueUpdateDraw(func() {
										modal.SetText("导出失败: " + err.Error())
										modal.AddButtons([]string{"OK"})
										modal.SetDoneFunc(func(buttonIndex int, buttonLabel string) {
											a.mainPages.RemovePage("modal")
										})
										a.SetFocus(modal)
									})
									return
								}

								// 导出为CSV
								outputPath := fmt.Sprintf("my_chatlog_%s.csv", time.Now().Format("20060102_150405"))
								if err := export.ExportMessages(messages, outputPath, "csv", func(current, total int) {
									percentage := float64(current) / float64(total) * 100
									width := 20 // 进度条宽度
									completed := int(float64(width) * float64(current) / float64(total))
									remaining := width - completed

									// 构建进度条
									progressBar := fmt.Sprintf("正在导出聊天记录\n\n[%s%s] %.1f%%\n(%d/%d)",
										strings.Repeat("█", completed),
										strings.Repeat("░", remaining),
										percentage,
										current,
										total)

									a.QueueUpdateDraw(func() {
										modal.SetText(progressBar)
									})
								}); err != nil {
									// 在主线程中更新UI
									a.QueueUpdateDraw(func() {
										modal.SetText("导出失败: " + err.Error())
										modal.AddButtons([]string{"OK"})
										modal.SetDoneFunc(func(buttonIndex int, buttonLabel string) {
											a.mainPages.RemovePage("modal")
										})
										a.SetFocus(modal)
									})
									return
								}

								// 在主线程中更新UI
								a.QueueUpdateDraw(func() {
									modal.SetText(fmt.Sprintf("导出成功\n文件已保存到: %s", outputPath))
									modal.AddButtons([]string{"OK"})
									modal.SetDoneFunc(func(buttonIndex int, buttonLabel string) {
										a.mainPages.RemovePage("modal")
									})
									a.SetFocus(modal)
								})
							}()
						},
					})

					a.mainPages.AddPage("submenu2", formatMenu, true, true)
					a.SetFocus(formatMenu)
				},
			})

			//// 导出所有图片
			//subMenu.AddItem(&menu.Item{
			//	Index:       4, // 设置一个唯一的索引
			//	Name:        "导出所有图片",
			//	Description: "导出当前账号的数据库中的所有图片",
			//	Selected: func(i *menu.Item) {
			//		// 显示导出中的模态框
			//		modal := tview.NewModal().SetText("正在导出图片...")
			//		a.mainPages.AddPage("modal", modal, true, true)
			//		a.SetFocus(modal)
			//
			//		defer func() {
			//			if r := recover(); r != nil {
			//				a.QueueUpdateDraw(func() {
			//					modal.SetText(fmt.Sprintf("导出异常: %v", r))
			//					modal.AddButtons([]string{"OK"})
			//				})
			//			}
			//		}()
			//
			//		// 在后台执行导出操作
			//		go func() {
			//			// 获取所有图片
			//			images, err := export.GetMediaFiles(a.m.db, "image", func(current, total int) {
			//				percentage := float64(current) / float64(total) * 100
			//				width := 20 // 进度条宽度
			//				completed := int(float64(width) * float64(current) / float64(total))
			//				remaining := width - completed
			//
			//				// 构建进度条
			//				progressBar := fmt.Sprintf("正在导出图片\n\n[%s%s] %.1f%%\n(%d/%d)",
			//					strings.Repeat("█", completed),
			//					strings.Repeat("░", remaining),
			//					percentage,
			//					current,
			//					total)
			//
			//				a.QueueUpdateDraw(func() {
			//					modal.SetText(progressBar)
			//				})
			//			})
			//			if err != nil {
			//				// 在主线程中更新UI
			//				a.QueueUpdateDraw(func() {
			//					modal.SetText("导出图片失败: " + err.Error())
			//					modal.AddButtons([]string{"OK"})
			//					modal.SetDoneFunc(func(buttonIndex int, buttonLabel string) {
			//						a.mainPages.RemovePage("modal")
			//					})
			//					a.SetFocus(modal)
			//				})
			//				return
			//			}
			//
			//			// 导出图片到指定目录
			//			outputDir := fmt.Sprintf("wechat_images_%s", time.Now().Format("20060102_150405"))
			//			if err := export.MediaFilesExport(images, outputDir, "image", func(current, total int) {
			//				percentage := float64(current) / float64(total) * 100
			//				width := 20 // 进度条宽度
			//				completed := int(float64(width) * float64(current) / float64(total))
			//				remaining := width - completed
			//
			//				// 构建进度条
			//				progressBar := fmt.Sprintf("正在导出图片\n\n[%s%s] %.1f%%\n(%d/%d)",
			//					strings.Repeat("█", completed),
			//					strings.Repeat("░", remaining),
			//					percentage,
			//					current,
			//					total)
			//
			//				a.QueueUpdateDraw(func() {
			//					modal.SetText(progressBar)
			//				})
			//			}); err != nil {
			//				// 在主线程中更新UI
			//				a.QueueUpdateDraw(func() {
			//					modal.SetText("导出图片失败: " + err.Error())
			//					modal.AddButtons([]string{"OK"})
			//					modal.SetDoneFunc(func(buttonIndex int, buttonLabel string) {
			//						a.mainPages.RemovePage("modal")
			//					})
			//					a.SetFocus(modal)
			//				})
			//				return
			//			}
			//
			//			// 在主线程中更新UI
			//			a.QueueUpdateDraw(func() {
			//				modal.SetText(fmt.Sprintf("图片导出成功\n文件已保存到: %s", outputDir))
			//				modal.AddButtons([]string{"OK"})
			//				modal.SetDoneFunc(func(buttonIndex int, buttonLabel string) {
			//					a.mainPages.RemovePage("modal")
			//				})
			//				a.SetFocus(modal)
			//			})
			//		}()
			//	},
			//})

			// 导出微信媒体文件
			subMenu.AddItem(&menu.Item{
				Index:       6, // 设置一个唯一的索引
				Name:        "导出微信媒体文件",
				Description: "导出微信的所有媒体文件到当前运行目录",
				Selected: func(i *menu.Item) {
					// 创建输入表单
					formView := form.NewForm("导出媒体")

					var talker, mediaType string

					// 添加输入字段 - 输入聊天ID
					formView.AddInputField("聊天ID", "", 0, nil, func(text string) {
						talker = text // 存储输入的聊天ID
					})

					// 添加输入字段 - 输入媒体类型(默认图片和视频) 0图片和视频 3图片 34语音 43视频 6文件
					formView.AddInputField("媒体类型", "0", 0, nil, func(text string) {
						mediaType = text // 存储输入的输入媒体类型
					})

					// 添加按钮 - 点击保存时开始导出
					formView.AddButton("导出", func() {
						//// 如果没有输入聊天ID，显示错误提示
						//if talker == "" {
						//	a.showError(fmt.Errorf("请输入群聊ID"))
						//	return
						//}

						a.mainPages.RemovePage("submenu2")

						// 提示文本
						tips := ""
						switch {
						case mediaType == "3":
							tips = "正在导出图片" + "..."
						case mediaType == "34":
							tips = "正在导出语音" + "..."
						case mediaType == "43":
							tips = "正在导出视频" + "..."
						case mediaType == "6":
							tips = "正在导出文件" + "..."
						default:
							tips = "正在导出图片和视频" + "..."
						}

						// 显示导出中的模态框
						modal := tview.NewModal().SetText(tips)
						a.mainPages.AddPage("modal", modal, true, true)
						a.SetFocus(modal)

						// 在后台执行导出操作
						go func() {
							// 获取指定聊天数据
							images, err := export.GetMessagesForExport(a.m.db, time.Time{}, time.Time{}, talker, false, func(current, total int, msg any) {
								percentage := float64(current) / float64(total) * 100
								width := 20 // 进度条宽度
								completed := int(float64(width) * float64(current) / float64(total))
								remaining := width - completed

								// 构建进度条
								progressBar := fmt.Sprintf("%s\n\n[%s%s] %.1f%%\n(%d/%d)\n 处理: %+v",
									tips,
									strings.Repeat("█", completed),
									strings.Repeat("░", remaining),
									percentage,
									current,
									total,
									msg,
								)

								a.QueueUpdateDraw(func() {
									modal.SetText(progressBar)
								})
							})
							if err != nil {
								// 在主线程中更新UI
								a.QueueUpdateDraw(func() {
									modal.SetText(fmt.Sprintf("%s失败: %+v", tips, err.Error()))
									modal.AddButtons([]string{"OK"})
									modal.SetDoneFunc(func(buttonIndex int, buttonLabel string) {
										a.mainPages.RemovePage("modal")
									})
									a.SetFocus(modal)
								})
								return
							}

							mediaTypeInt64, err := strconv.ParseInt(mediaType, 10, 64)
							if err != nil {
								// 转换失败，默认导出图片和视频
								mediaTypeInt64 = 0
							}

							msgMedias, err := export.GetMessageMedia(a.m.db, mediaTypeInt64, images...)
							if err != nil {
								// 在主线程中更新UI
								a.QueueUpdateDraw(func() {
									modal.SetText(fmt.Sprintf("%s失败: %+v", "获取媒体信息", err.Error()))
									modal.AddButtons([]string{"OK"})
									modal.SetDoneFunc(func(buttonIndex int, buttonLabel string) {
										a.mainPages.RemovePage("modal")
									})
									a.SetFocus(modal)
								})
								return
							}

							// 导出图片到指定目录
							outputDir := fmt.Sprintf("wechat_media_%s_%s", mediaType, time.Now().Format("20060102_150405"))
							if err = export.MediaFilesExport(msgMedias, a.ctx.DataDir, outputDir, "image", func(current, total int) {
								percentage := float64(current) / float64(total) * 100
								width := 20 // 进度条宽度
								completed := int(float64(width) * float64(current) / float64(total))
								remaining := width - completed

								// 构建进度条
								progressBar := fmt.Sprintf("%s\n\n[%s%s] %.1f%%\n(%d/%d)",
									tips,
									strings.Repeat("█", completed),
									strings.Repeat("░", remaining),
									percentage,
									current,
									total)

								a.QueueUpdateDraw(func() {
									modal.SetText(progressBar)
								})
							}); err != nil {
								// 在主线程中更新UI
								a.QueueUpdateDraw(func() {
									modal.SetText("导出图片失败: " + err.Error())
									modal.AddButtons([]string{"OK"})
									modal.SetDoneFunc(func(buttonIndex int, buttonLabel string) {
										a.mainPages.RemovePage("modal")
									})
									a.SetFocus(modal)
								})
								return
							}

							// 在主线程中更新UI
							a.QueueUpdateDraw(func() {
								modal.SetText(fmt.Sprintf("图片导出成功\n文件已保存到: %s", outputDir))
								modal.AddButtons([]string{"OK"})
								modal.SetDoneFunc(func(buttonIndex int, buttonLabel string) {
									a.mainPages.RemovePage("modal")
								})
								a.SetFocus(modal)
							})
						}()
					})

					// 添加取消按钮
					formView.AddButton("取消", func() {
						a.mainPages.RemovePage("submenu2")
					})

					a.mainPages.AddPage("submenu2", formView, true, true)
					a.SetFocus(formView)
				},
			})

			// 添加导出群聊图片菜单项
			subMenu.AddItem(&menu.Item{
				Index:       7, // 设置一个唯一的索引
				Name:        "导出群聊图片",
				Description: "导出指定群聊的所有图片",
				Selected: func(i *menu.Item) {
					// 创建输入表单
					formView := form.NewForm("导出群聊图片")

					var talker string

					// 添加输入字段 - 输入群聊ID
					formView.AddInputField("群聊ID", "", 0, nil, func(text string) {
						talker = text // 存储输入的群聊ID
					})

					// 添加按钮 - 点击保存时开始导出
					formView.AddButton("导出", func() {
						// 如果没有输入群聊ID，显示错误提示
						if talker == "" {
							a.showError(fmt.Errorf("请输入群聊ID"))
							return
						}

						a.mainPages.RemovePage("submenu2")

						// 显示导出中的模态框
						modal := tview.NewModal().SetText("正在导出群聊图片...")
						a.mainPages.AddPage("modal", modal, true, true)
						a.SetFocus(modal)

						// 在后台执行导出操作
						go func() {
							// 获取指定群聊的图片
							images, err := export.GetGroupMediaFiles(a.m.db, talker, export.TypeImage, func(current, total int, msg any) {
								percentage := float64(current) / float64(total) * 100
								width := 20 // 进度条宽度
								completed := int(float64(width) * float64(current) / float64(total))
								remaining := width - completed

								// 构建进度条
								progressBar := fmt.Sprintf("正在导出图片\n\n[%s%s] %.1f%%\n(%d/%d)\n\n消息内容: %+v",
									strings.Repeat("█", completed),
									strings.Repeat("░", remaining),
									percentage,
									current,
									total,
									msg,
								)

								a.QueueUpdateDraw(func() {
									modal.SetText(progressBar)
								})
							})
							if err != nil {
								// 在主线程中更新UI
								a.QueueUpdateDraw(func() {
									modal.SetText("导出图片失败: " + err.Error())
									modal.AddButtons([]string{"OK"})
									modal.SetDoneFunc(func(buttonIndex int, buttonLabel string) {
										a.mainPages.RemovePage("modal")
									})
									a.SetFocus(modal)
								})
								return
							}

							// 导出图片到指定目录
							outputDir := fmt.Sprintf("wechat_group_images_%s", time.Now().Format("20060102_150405"))
							if err := export.MediaFilesExport(images, a.ctx.DataDir, outputDir, "image", func(current, total int) {
								percentage := float64(current) / float64(total) * 100
								width := 20 // 进度条宽度
								completed := int(float64(width) * float64(current) / float64(total))
								remaining := width - completed

								// 构建进度条
								progressBar := fmt.Sprintf("正在导出图片\n\n[%s%s] %.1f%%\n(%d/%d)",
									strings.Repeat("█", completed),
									strings.Repeat("░", remaining),
									percentage,
									current,
									total)

								a.QueueUpdateDraw(func() {
									modal.SetText(progressBar)
								})
							}); err != nil {
								// 在主线程中更新UI
								a.QueueUpdateDraw(func() {
									modal.SetText("导出图片失败: " + err.Error())
									modal.AddButtons([]string{"OK"})
									modal.SetDoneFunc(func(buttonIndex int, buttonLabel string) {
										a.mainPages.RemovePage("modal")
									})
									a.SetFocus(modal)
								})
								return
							}

							// 在主线程中更新UI
							a.QueueUpdateDraw(func() {
								modal.SetText(fmt.Sprintf("图片导出成功\n文件已保存到: %s", outputDir))
								modal.AddButtons([]string{"OK"})
								modal.SetDoneFunc(func(buttonIndex int, buttonLabel string) {
									a.mainPages.RemovePage("modal")
								})
								a.SetFocus(modal)
							})
						}()
					})

					// 添加取消按钮
					formView.AddButton("取消", func() {
						a.mainPages.RemovePage("submenu2")
					})

					a.mainPages.AddPage("submenu2", formView, true, true)
					a.SetFocus(formView)
				},
			})

			a.mainPages.AddPage("submenu", subMenu, true, true)
			a.SetFocus(subMenu)
		},
	}

	selectAccount := &menu.Item{
		Index:       8,
		Name:        "切换账号",
		Description: "切换当前操作的账号，可以选择进程或历史账号",
		Selected:    a.selectAccountSelected,
	}

	a.menu.AddItem(getDataKey)
	a.menu.AddItem(decryptData)
	a.menu.AddItem(httpServer)
	a.menu.AddItem(autoDecrypt)
	a.menu.AddItem(setting)
	a.menu.AddItem(export)
	a.menu.AddItem(selectAccount)

	a.menu.AddItem(&menu.Item{
		Index:       9,
		Name:        "退出",
		Description: "退出程序",
		Selected: func(i *menu.Item) {
			a.Stop()
		},
	})
}

// settingItem 表示一个设置项
type settingItem struct {
	name        string
	description string
	action      func()
}

func (a *App) settingSelected(i *menu.Item) {

	settings := []settingItem{
		{
			name:        "设置 HTTP 服务地址",
			description: "配置 HTTP 服务监听的地址",
			action:      a.settingHTTPPort,
		},
		{
			name:        "设置工作目录",
			description: "配置数据解密后的存储目录",
			action:      a.settingWorkDir,
		},
		{
			name:        "设置数据密钥",
			description: "配置数据解密密钥",
			action:      a.settingDataKey,
		},
		{
			name:        "设置数据目录",
			description: "配置微信数据文件所在目录",
			action:      a.settingDataDir,
		},
	}

	subMenu := menu.NewSubMenu("设置")
	for idx, setting := range settings {
		item := &menu.Item{
			Index:       idx + 1,
			Name:        setting.name,
			Description: setting.description,
			Selected: func(action func()) func(*menu.Item) {
				return func(*menu.Item) {
					action()
				}
			}(setting.action),
		}
		subMenu.AddItem(item)
	}

	a.mainPages.AddPage("submenu", subMenu, true, true)
	a.SetFocus(subMenu)
}

// settingHTTPPort 设置 HTTP 端口
func (a *App) settingHTTPPort() {
	// 使用我们的自定义表单组件
	formView := form.NewForm("设置 HTTP 地址")

	// 临时存储用户输入的值
	tempHTTPAddr := a.ctx.HTTPAddr

	// 添加输入字段 - 不再直接设置HTTP地址，而是更新临时变量
	formView.AddInputField("地址", tempHTTPAddr, 0, nil, func(text string) {
		tempHTTPAddr = text // 只更新临时变量
	})

	// 添加按钮 - 点击保存时才设置HTTP地址
	formView.AddButton("保存", func() {
		a.m.SetHTTPAddr(tempHTTPAddr) // 在这里设置HTTP地址
		a.mainPages.RemovePage("submenu2")
		a.showInfo("HTTP 地址已设置为 " + a.ctx.HTTPAddr)
	})

	formView.AddButton("取消", func() {
		a.mainPages.RemovePage("submenu2")
	})

	a.mainPages.AddPage("submenu2", formView, true, true)
	a.SetFocus(formView)
}

// settingWorkDir 设置工作目录
func (a *App) settingWorkDir() {
	// 使用我们的自定义表单组件
	formView := form.NewForm("设置工作目录")

	// 临时存储用户输入的值
	tempWorkDir := a.ctx.WorkDir

	// 添加输入字段 - 不再直接设置工作目录，而是更新临时变量
	formView.AddInputField("工作目录", tempWorkDir, 0, nil, func(text string) {
		tempWorkDir = text // 只更新临时变量
	})

	// 添加按钮 - 点击保存时才设置工作目录
	formView.AddButton("保存", func() {
		a.ctx.SetWorkDir(tempWorkDir) // 在这里设置工作目录
		a.mainPages.RemovePage("submenu2")
		a.showInfo("工作目录已设置为 " + a.ctx.WorkDir)
	})

	formView.AddButton("取消", func() {
		a.mainPages.RemovePage("submenu2")
	})

	a.mainPages.AddPage("submenu2", formView, true, true)
	a.SetFocus(formView)
}

// settingDataKey 设置数据密钥
func (a *App) settingDataKey() {
	// 使用我们的自定义表单组件
	formView := form.NewForm("设置数据密钥")

	// 临时存储用户输入的值
	tempDataKey := a.ctx.DataKey

	// 添加输入字段 - 不直接设置数据密钥，而是更新临时变量
	formView.AddInputField("数据密钥", tempDataKey, 0, nil, func(text string) {
		tempDataKey = text // 只更新临时变量
	})

	// 添加按钮 - 点击保存时才设置数据密钥
	formView.AddButton("保存", func() {
		a.ctx.DataKey = tempDataKey // 设置数据密钥
		a.mainPages.RemovePage("submenu2")
		a.showInfo("数据密钥已设置")
	})

	formView.AddButton("取消", func() {
		a.mainPages.RemovePage("submenu2")
	})

	a.mainPages.AddPage("submenu2", formView, true, true)
	a.SetFocus(formView)
}

// settingDataDir 设置数据目录
func (a *App) settingDataDir() {
	// 使用我们的自定义表单组件
	formView := form.NewForm("设置数据目录")

	// 临时存储用户输入的值
	tempDataDir := a.ctx.DataDir

	// 添加输入字段 - 不直接设置数据目录，而是更新临时变量
	formView.AddInputField("数据目录", tempDataDir, 0, nil, func(text string) {
		tempDataDir = text // 只更新临时变量
	})

	// 添加按钮 - 点击保存时才设置数据目录
	formView.AddButton("保存", func() {
		a.ctx.DataDir = tempDataDir // 设置数据目录
		a.mainPages.RemovePage("submenu2")
		a.showInfo("数据目录已设置为 " + a.ctx.DataDir)
	})

	formView.AddButton("取消", func() {
		a.mainPages.RemovePage("submenu2")
	})

	a.mainPages.AddPage("submenu2", formView, true, true)
	a.SetFocus(formView)
}

// selectAccountSelected 处理切换账号菜单项的选择事件
func (a *App) selectAccountSelected(i *menu.Item) {
	// 创建子菜单
	subMenu := menu.NewSubMenu("切换账号")

	// 添加微信进程
	instances := a.m.wechat.GetWeChatInstances()
	if len(instances) > 0 {
		// 添加实例标题
		subMenu.AddItem(&menu.Item{
			Index:       0,
			Name:        "--- 微信进程 ---",
			Description: "",
			Hidden:      false,
			Selected:    nil,
		})

		// 添加实例列表
		for idx, instance := range instances {
			// 创建一个实例描述
			description := fmt.Sprintf("版本: %s 目录: %s", instance.FullVersion, instance.DataDir)

			// 标记当前选中的实例
			name := fmt.Sprintf("%s [%d]", instance.Name, instance.PID)
			if a.ctx.Current != nil && a.ctx.Current.PID == instance.PID {
				name = name + " [当前]"
			}

			// 创建菜单项
			instanceItem := &menu.Item{
				Index:       idx + 1,
				Name:        name,
				Description: description,
				Hidden:      false,
				Selected: func(instance *wechat.Account) func(*menu.Item) {
					return func(*menu.Item) {
						// 如果是当前账号，则无需切换
						if a.ctx.Current != nil && a.ctx.Current.PID == instance.PID {
							a.mainPages.RemovePage("submenu")
							a.showInfo("已经是当前账号")
							return
						}

						// 显示切换中的模态框
						modal := tview.NewModal().SetText("正在切换账号...")
						a.mainPages.AddPage("modal", modal, true, true)
						a.SetFocus(modal)

						// 在后台执行切换操作
						go func() {
							err := a.m.Switch(instance, "")

							// 在主线程中更新UI
							a.QueueUpdateDraw(func() {
								a.mainPages.RemovePage("modal")
								a.mainPages.RemovePage("submenu")

								if err != nil {
									// 切换失败
									a.showError(fmt.Errorf("切换账号失败: %v", err))
								} else {
									// 切换成功
									a.showInfo("切换账号成功")
									// 更新菜单状态
									a.updateMenuItemsState()
								}
							})
						}()
					}
				}(instance),
			}
			subMenu.AddItem(instanceItem)
		}
	}

	// 添加历史账号
	if len(a.ctx.History) > 0 {
		// 添加历史账号标题
		subMenu.AddItem(&menu.Item{
			Index:       100,
			Name:        "--- 历史账号 ---",
			Description: "",
			Hidden:      false,
			Selected:    nil,
		})

		// 添加历史账号列表
		idx := 101
		for account, hist := range a.ctx.History {
			// 创建一个账号描述
			description := fmt.Sprintf("版本: %s 目录: %s", hist.FullVersion, hist.DataDir)

			// 标记当前选中的账号
			name := account
			if name == "" {
				name = filepath.Base(hist.DataDir)
			}
			if a.ctx.DataDir == hist.DataDir {
				name = name + " [当前]"
			}

			// 创建菜单项
			histItem := &menu.Item{
				Index:       idx,
				Name:        name,
				Description: description,
				Hidden:      false,
				Selected: func(account string) func(*menu.Item) {
					return func(*menu.Item) {
						// 如果是当前账号，则无需切换
						if a.ctx.Current != nil && a.ctx.DataDir == a.ctx.History[account].DataDir {
							a.mainPages.RemovePage("submenu")
							a.showInfo("已经是当前账号")
							return
						}

						// 显示切换中的模态框
						modal := tview.NewModal().SetText("正在切换账号...")
						a.mainPages.AddPage("modal", modal, true, true)
						a.SetFocus(modal)

						// 在后台执行切换操作
						go func() {
							err := a.m.Switch(nil, account)

							// 在主线程中更新UI
							a.QueueUpdateDraw(func() {
								a.mainPages.RemovePage("modal")
								a.mainPages.RemovePage("submenu")

								if err != nil {
									// 切换失败
									a.showError(fmt.Errorf("切换账号失败: %v", err))
								} else {
									// 切换成功
									a.showInfo("切换账号成功")
									// 更新菜单状态
									a.updateMenuItemsState()
								}
							})
						}()
					}
				}(account),
			}
			idx++
			subMenu.AddItem(histItem)
		}
	}

	// 如果没有账号可选择
	if len(a.ctx.History) == 0 && len(instances) == 0 {
		subMenu.AddItem(&menu.Item{
			Index:       1,
			Name:        "无可用账号",
			Description: "未检测到微信进程或历史账号",
			Hidden:      false,
			Selected:    nil,
		})
	}

	// 显示子菜单
	a.mainPages.AddPage("submenu", subMenu, true, true)
	a.SetFocus(subMenu)
}

// showModal 显示一个模态对话框
func (a *App) showModal(text string, buttons []string, doneFunc func(buttonIndex int, buttonLabel string)) {
	modal := tview.NewModal().
		SetText(text).
		AddButtons(buttons).
		SetDoneFunc(doneFunc)

	a.mainPages.AddPage("modal", modal, true, true)
	a.SetFocus(modal)
}

// showError 显示错误对话框
func (a *App) showError(err error) {
	a.showModal(err.Error(), []string{"OK"}, func(buttonIndex int, buttonLabel string) {
		a.mainPages.RemovePage("modal")
	})
}

// showInfo 显示信息对话框
func (a *App) showInfo(text string) {
	a.showModal(text, []string{"OK"}, func(buttonIndex int, buttonLabel string) {
		a.mainPages.RemovePage("modal")
	})
}
