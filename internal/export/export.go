package export

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/sjzar/chatlog/pkg/util/dat2img"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/sjzar/chatlog/internal/model"
	"github.com/sjzar/chatlog/internal/wechatdb"
)

// ProgressCallback 用于报告导出进度的回调函数
type ProgressCallback func(current, total int)

// ProgressCallbackMsg 用于报告导出进度的回调函数带回调信息
type ProgressCallbackMsg func(current, total int, cbMsg any)

// ExportMessages 导出消息到文件
func ExportMessages(messages []*model.Message, outputPath string, format string, progress ProgressCallback) error {
	switch format {
	case "json":
		return exportJSON(messages, outputPath, progress)
	case "csv":
		return exportCSV(messages, outputPath, progress)
	default:
		return fmt.Errorf("unsupported format: %s", format)
	}
}

// GetMessagesForExport 获取要导出的消息
func GetMessagesForExport(db interface {
	GetMessages(startTime, endTime time.Time, talker, sender, content string, offset, limit int) ([]*model.Message, error)
	GetContacts(keyword string, offset, limit int) (*wechatdb.GetContactsResp, error)
}, startTime, endTime time.Time, talker string, onlySelf bool, progress ProgressCallbackMsg) ([]*model.Message, error) {
	// 如果没有指定时间范围，默认从2010年到现在
	if startTime.IsZero() {
		startTime, _ = time.Parse("2006-01-02", "2010-01-01")
	}
	if endTime.IsZero() {
		endTime = time.Now()
	}

	// 如果指定了联系人，直接获取该联系人的消息
	if talker != "" {
		msgs, err := db.GetMessages(startTime, endTime, talker, "", "", 0, 0)
		if err != nil {
			return nil, err
		}
		if onlySelf {
			return filterSelfMessages(msgs), nil
		}
		return msgs, nil
	}

	// 获取所有联系人
	contacts, err := db.GetContacts("", 0, 0)
	if err != nil {
		return nil, err
	}

	// 检查联系人列表是否为空
	if contacts == nil || len(contacts.Items) == 0 {
		return nil, fmt.Errorf("no contacts found")
	}

	// 获取所有聊天记录
	var allMessages []*model.Message
	totalContacts := len(contacts.Items)

	for i, contact := range contacts.Items {
		// 跳过没有用户名的联系人
		if contact.UserName == "" {
			continue
		}

		// 更新进度：获取联系人列表的进度
		if progress != nil {
			progress(i+1, totalContacts, contact.NickName)
		}

		// 获取该联系人的聊天记录
		msgs, err := db.GetMessages(startTime, endTime, contact.UserName, "", "", 0, 0)
		if err != nil {
			log.Error().Err(err).Str("contact", contact.UserName).Msg("failed to get messages")
			continue
		}

		// 如果成功获取到消息，添加到列表中
		if len(msgs) > 0 {
			if onlySelf {
				allMessages = append(allMessages, filterSelfMessages(msgs)...)
			} else {
				allMessages = append(allMessages, msgs...)
			}
			log.Info().Str("contact", contact.UserName).Int("count", len(msgs)).Msg("successfully got messages")
		}
	}

	if len(allMessages) == 0 {
		return nil, fmt.Errorf("no messages found")
	}

	return allMessages, nil
}

// filterSelfMessages 过滤出自己发送的消息
func filterSelfMessages(messages []*model.Message) []*model.Message {
	var selfMessages []*model.Message
	for _, msg := range messages {
		if msg.IsSelf {
			selfMessages = append(selfMessages, msg)
		}
	}
	return selfMessages
}

// MessageType 消息类型常量
const (
	TypeText   = 1     // 文本消息
	TypeImage  = 3     // 图片消息
	TypeVoice  = 34    // 语音消息
	TypeVideo  = 43    // 视频消息
	TypeApp    = 49    // 应用消息
	TypeSystem = 10000 // 系统消息
)

// AppMessageSubType 应用消息子类型常量
const (
	SubTypeLink     = 5  // 链接分享
	SubTypeFile     = 6  // 文件
	SubTypeForward  = 19 // 合并转发
	SubTypeMiniApp  = 33 // 小程序
	SubTypeMiniApp2 = 36 // 小程序
	SubTypeVideo    = 51 // 视频号
	SubTypeQuote    = 57 // 引用消息
	SubTypePat      = 62 // 拍一拍
)

// GetMessageTypeDesc 将消息类型转换为可读的中文描述
func GetMessageTypeDesc(msg *model.Message) string {
	// 基础消息类型描述
	typeDesc := map[int64]string{
		TypeText:   "文本消息",
		TypeImage:  "图片消息",
		TypeVoice:  "语音消息",
		TypeVideo:  "视频消息",
		TypeSystem: "系统消息",
	}

	// 如果是基础消息类型，直接返回描述
	if desc, ok := typeDesc[msg.Type]; ok {
		return desc
	}

	// 如果是应用消息，需要根据子类型返回描述
	if msg.Type == TypeApp {
		subTypeDesc := map[int64]string{
			SubTypeLink:     "链接分享",
			SubTypeFile:     "文件",
			SubTypeForward:  "合并转发",
			SubTypeMiniApp:  "小程序",
			SubTypeMiniApp2: "小程序",
			SubTypeVideo:    "视频号",
			SubTypeQuote:    "引用消息",
			SubTypePat:      "拍一拍",
		}

		if desc, ok := subTypeDesc[msg.SubType]; ok {
			return desc
		}
		return fmt.Sprintf("应用消息(%d)", msg.SubType)
	}

	// 未知消息类型
	return fmt.Sprintf("未知类型(%d)", msg.Type)
}

// MessageWithDesc 带描述的消息结构
type MessageWithDesc struct {
	Seq        int64                  `json:"seq"`
	Time       time.Time              `json:"time"`
	Talker     string                 `json:"talker"`
	TalkerName string                 `json:"talkerName"`
	IsChatRoom bool                   `json:"isChatRoom"`
	Sender     string                 `json:"sender"`
	SenderName string                 `json:"senderName"`
	IsSelf     bool                   `json:"isSelf"`
	Type       int64                  `json:"type"`
	SubType    int64                  `json:"subType"`
	Content    string                 `json:"content"`
	Contents   map[string]interface{} `json:"contents,omitempty"`
	TypeDesc   string                 `json:"typeDesc"`
}

func exportJSON(messages []*model.Message, outputPath string, progress ProgressCallback) error {
	file, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer file.Close()

	total := len(messages)
	messagesWithDesc := make([]MessageWithDesc, total)

	// 批量处理消息，每100条更新一次进度
	batchSize := 100
	lastUpdate := time.Now()

	for i, msg := range messages {
		messagesWithDesc[i] = MessageWithDesc{
			Seq:        msg.Seq,
			Time:       msg.Time,
			Talker:     msg.Talker,
			TalkerName: msg.TalkerName,
			IsChatRoom: msg.IsChatRoom,
			Sender:     msg.Sender,
			SenderName: msg.SenderName,
			IsSelf:     msg.IsSelf,
			Type:       msg.Type,
			SubType:    msg.SubType,
			Content:    msg.Content,
			Contents:   msg.Contents,
			TypeDesc:   GetMessageTypeDesc(msg),
		}

		// 每处理batchSize条消息或距离上次更新超过100ms才更新进度
		if progress != nil && (i%batchSize == 0 || time.Since(lastUpdate) > 100*time.Millisecond) {
			progress(i+1, total)
			lastUpdate = time.Now()
		}
	}

	// 确保最后更新一次进度
	if progress != nil {
		progress(total, total)
	}

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	return encoder.Encode(messagesWithDesc)
}

func exportCSV(messages []*model.Message, outputPath string, progress ProgressCallback) error {
	file, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	// 写入CSV头
	headers := []string{"Time", "Talker", "TalkerName", "Sender", "SenderName", "IsSelf", "Type", "TypeDesc", "Content"}
	if err := writer.Write(headers); err != nil {
		return err
	}

	total := len(messages)
	// 批量处理消息，每100条更新一次进度
	batchSize := 100
	lastUpdate := time.Now()

	// 写入数据
	for i, msg := range messages {
		record := []string{
			msg.Time.Format("2006-01-02 15:04:05"),
			msg.Talker,
			msg.TalkerName,
			msg.Sender,
			msg.SenderName,
			fmt.Sprintf("%v", msg.IsSelf),
			fmt.Sprintf("%d", msg.Type),
			GetMessageTypeDesc(msg),
			msg.Content,
		}
		if err := writer.Write(record); err != nil {
			return err
		}

		// 每处理batchSize条消息或距离上次更新超过100ms才更新进度
		if progress != nil && (i%batchSize == 0 || time.Since(lastUpdate) > 100*time.Millisecond) {
			progress(i+1, total)
			lastUpdate = time.Now()
		}
	}

	// 确保最后更新一次进度
	if progress != nil {
		progress(total, total)
	}

	return nil
}

// MsgMediaExport 媒体消息（如图片或视频）
type MsgMediaExport struct {
	//ID         int64     `json:"id"`         // 文件ID
	//Type       string    `json:"type"`       // 文件类型（image, video等）
	//Path       string    `json:"path"`       // 文件路径
	//Size       int64     `json:"size"`       // 文件大小（字节）
	//CreateTime time.Time `json:"createTime"` // 创建时间

	Msg   *model.Message `json:"msg"`   // 消息
	Media *model.Media   `json:"media"` // 媒体信息
}

// GetMessageMedia 获取消息的关联媒体文件信息
func GetMessageMedia(
	db interface {
		GetMedia(_type string, key string) (*model.Media, error)
	},
	mediaType int64,
	messages ...*model.Message,
) ([]*MsgMediaExport, error) {

	filterMessages := make([]*model.Message, 0)
	// 过滤掉非指定类型的消息
	for _, msg := range messages {
		switch {
		case msg.Type == mediaType:
			filterMessages = append(filterMessages, msg)
		default:
			// 默认导出图片和视频
			if mediaType == TypeImage || mediaType == TypeVideo {
				filterMessages = append(filterMessages, msg)
			}
		}
	}

	var mediaFiles []*MsgMediaExport

	// 处理过滤后的消息
	for _, msg := range filterMessages {
		// 获取媒体信息
		_type, md5 := "", ""
		if _md5, ok := msg.Contents["md5"].(string); ok && _md5 != "" {
			md5 = _md5
		} else {
			return nil, errors.New("err: md5 is empty")
		}

		// 根据消息类型处理媒体
		switch msg.Type {
		case TypeImage:
			_type = "image"
		case TypeVideo:
			_type = "video"
		case SubTypeFile:
			// TODO 文件
			_type, md5 = "file", ""
		case TypeVoice:
			// TODO 语音
			_type, md5 = "voice", ""
		default:
			continue
		}

		// 查询媒体
		media, err := db.GetMedia(_type, md5)
		if err != nil {
			// 忽略错误
			continue
			//return nil, errors.New(err, 1, fmt.Sprintf("Err: %+v, msg: %+v", err, msg.Contents))
		}

		m := &MsgMediaExport{
			Msg:   msg,
			Media: media,
		}

		mediaFiles = append(mediaFiles, m)
	}

	if len(mediaFiles) == 0 {
		return nil, fmt.Errorf("获取媒体信息为空")
	}

	return mediaFiles, nil
}

// GetGroupMediaFiles 获取指定群聊的媒体文件
func GetGroupMediaFiles(db interface {
	GetMessages(startTime, endTime time.Time, talker, sender, content string, offset, limit int) ([]*model.Message, error)
	GetContacts(keyword string, offset, limit int) (*wechatdb.GetContactsResp, error)
	GetMedia(_type string, key string) (*model.Media, error)
}, talker string, mediaType int, progress func(current int, total int, msg any)) ([]*MsgMediaExport, error) {
	// 如果没有指定媒体类型，默认获取所有类型的媒体文件
	if mediaType == 0 {
		return nil, fmt.Errorf("media type must be specified")
	}

	// 根据不同的媒体类型定义消息类型
	var msgType int64
	switch mediaType {
	case TypeImage:
		msgType = TypeImage // 图片消息类型
	case TypeVideo:
		msgType = TypeVideo // 视频消息类型
	default:
		return nil, fmt.Errorf("unsupported media type: %s", mediaType)
	}

	// 设置时间范围
	startTime := time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)
	endTime := time.Now() // time.Date(2025, time.December, 30, 23, 59, 59, 0, time.UTC)

	// 获取该群聊的聊天记录
	messages, err := db.GetMessages(startTime, endTime, talker, "", "", 0, 0)
	if err != nil {
		log.Error().Err(err).Str("contact", talker).Msg("failed to get group messages")
		return nil, err
	}

	// 创建一个新的切片，只包含指定类型的消息
	newMessages := make([]*model.Message, 0)
	// 过滤掉非指定类型的消息
	for _, msg := range messages {
		if msg.Type == msgType {
			newMessages = append(newMessages, msg)
		}
	}

	// 遍历消息，筛选出指定类型的媒体文件
	var mediaFiles []*MsgMediaExport
	for i, msg := range newMessages {
		var filePath string
		if msg.Type == msgType {
			// 优先使用 imgfile，回退到 MD5
			if imgfile, ok := msg.Contents["imgfile"].(string); ok && imgfile != "" {
				filePath = imgfile
			} else if md5, ok := msg.Contents["md5"].(string); ok && md5 != "" {
				filePath = md5
			}
		} else if msg.Type == msgType {
			// 类似处理视频文件
			if videofile, ok := msg.Contents["videofile"].(string); ok && videofile != "" {
				filePath = videofile
			}
		}

		// 查询媒体
		media, err := db.GetMedia("image", filePath)
		if err != nil {
			// 忽略错误
			continue
			//return nil, errors.New(err, 1, fmt.Sprintf("Err: %+v, msg: %+v", err, msg.Contents))
		}

		m := &MsgMediaExport{
			//ID:         0,
			//Type:       "",
			//Path:       "",
			//Size:       0,
			//CreateTime: time.Time{},
			Msg:   msg,
			Media: media,
		}

		mediaFiles = append(mediaFiles, m)

		// 更新进度：获取聊天列表的进度
		if progress != nil {
			progress(i+1, len(newMessages), fmt.Sprintf("%+v", media))
		}
	}

	if len(mediaFiles) == 0 {
		return nil, fmt.Errorf("no media files found in the specified group")
	}

	return mediaFiles, nil
}

// MediaFilesExport 将媒体文件导出到指定目录
func MediaFilesExport(mediaFiles []*MsgMediaExport, dataDir, outputDir, mediaType string, progress ProgressCallback) error {
	// 创建输出目录
	if err := os.MkdirAll(outputDir, os.ModePerm); err != nil {
		return fmt.Errorf("创建输出目录失败: %w", err)
	}

	total := len(mediaFiles)
	if total == 0 {
		return nil // 空列表直接返回
	}

	// 遍历并复制每个文件
	for i, mediaFile := range mediaFiles {
		if mediaFile == nil {
			continue // 跳过空对象
		}

		// 检查源文件路径
		absolutePath := filepath.Join(dataDir, mediaFile.Media.Path)
		if _, err := os.Stat(absolutePath); os.IsNotExist(err) {
			return fmt.Errorf("源文件不存在: %s", absolutePath)
		}

		// 获取源文件扩展名
		ext := strings.ToLower(filepath.Ext(absolutePath))

		// 提取年月信息并构建目录
		yearDir := filepath.Join(outputDir, strconv.Itoa(mediaFile.Msg.Time.Year()))
		monthDir := filepath.Join(yearDir, strconv.Itoa(int(mediaFile.Msg.Time.Month())))

		// 创建年月目录
		if err := os.MkdirAll(monthDir, os.ModePerm); err != nil {
			return fmt.Errorf("创建年月目录失败: %w", err)
		}

		switch {
		case ext == ".dat":
			// 处理.dat文件
			b, err := os.ReadFile(absolutePath)
			if err != nil {
				return fmt.Errorf("无法读取文件: %s", absolutePath)
			}

			// 转换为图片(获取真实文件、扩展名)
			dat2Out, dat2Ext, err := dat2img.Dat2Image(b)
			if err != nil {
				return fmt.Errorf("无法转换文件: %s", absolutePath)
			}

			// 构建目标路径
			dstPath := filepath.Join(monthDir, fmt.Sprintf("%s.%s", mediaFile.Media.Key, dat2Ext))

			// 写入文件
			if err = os.WriteFile(dstPath, dat2Out, os.ModePerm); err != nil {
				return fmt.Errorf("无法保存文件 %s: %w", dstPath, err)
			}
		default:
			// 原始文件直接复制
			srcPath := absolutePath
			// 构建目标路径（包含年月目录）
			dstPath := filepath.Join(monthDir, fmt.Sprintf("%s%s", mediaFile.Media.Key, ext))

			// 复制文件并处理错误
			if err := copyFile(srcPath, dstPath); err != nil {
				return fmt.Errorf("文件复制失败, Msg: %+v | %s -> %s: %w", mediaFile, srcPath, dstPath, err)
			}
		}

		//dstPath := filepath.Join(outputDir, fmt.Sprintf("%s%s", mediaFile.Key, fileExt))
		//
		//// 复制文件并处理错误
		//if err := copyFile(srcPath, dstPath); err != nil {
		//	return fmt.Errorf("文件复制失败 %s -> %s: %w", srcPath, dstPath, err)
		//}

		// 调用进度回调（每处理10个文件或最后一条记录更新一次进度）
		if progress != nil && (i%10 == 0 || i == total-1) {
			progress(i+1, total)
		}
	}

	return nil
}

// copyFile 实现文件复制功能
func copyFile(src, dst string) error {
	// 打开源文件
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	// 创建目标文件
	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	// 复制文件内容
	_, err = io.Copy(dstFile, srcFile)
	return err
}
