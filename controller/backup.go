package controller

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// BackupMeta 备份文件元数据
type BackupMeta struct {
	Version     string `json:"version"`
	CreatedAt   int64  `json:"created_at"`
	CreatedBy   string `json:"created_by"`
	Description string `json:"description,omitempty"`
}

// BackupData 备份数据
type BackupData struct {
	Channels      []ChannelBackup      `json:"channels,omitempty"`
	Users         []UserBackup         `json:"users,omitempty"`
	Tokens        []TokenBackup        `json:"tokens,omitempty"`
	Options       []OptionBackup       `json:"options,omitempty"`
	PrefillGroups []PrefillGroupBackup `json:"prefill_groups,omitempty"`
}

// BackupFile 完整备份文件结构
type BackupFile struct {
	Meta BackupMeta `json:"meta"`
	Data BackupData `json:"data"`
}

// ChannelBackup 渠道备份结构
type ChannelBackup struct {
	Id                 int                 `json:"id"`
	Type               int                 `json:"type"`
	Key                string              `json:"key"`
	OpenAIOrganization *string             `json:"openai_organization,omitempty"`
	TestModel          *string             `json:"test_model,omitempty"`
	Status             int                 `json:"status"`
	Name               string              `json:"name"`
	Weight             *uint               `json:"weight,omitempty"`
	CreatedTime        int64               `json:"created_time"`
	BaseURL            *string             `json:"base_url,omitempty"`
	Other              string              `json:"other,omitempty"`
	Models             string              `json:"models"`
	Group              string              `json:"group"`
	ModelMapping       *string             `json:"model_mapping,omitempty"`
	StatusCodeMapping  *string             `json:"status_code_mapping,omitempty"`
	Priority           *int64              `json:"priority,omitempty"`
	AutoBan            *int                `json:"auto_ban,omitempty"`
	OtherInfo          string              `json:"other_info,omitempty"`
	Tag                *string             `json:"tag,omitempty"`
	Setting            *string             `json:"setting,omitempty"`
	ParamOverride      *string             `json:"param_override,omitempty"`
	HeaderOverride     *string             `json:"header_override,omitempty"`
	Remark             *string             `json:"remark,omitempty"`
	ChannelInfo        model.ChannelInfo   `json:"channel_info"`
	OtherSettings      string              `json:"settings,omitempty"`
}

// UserBackup 用户备份结构
type UserBackup struct {
	Id              int    `json:"id"`
	Username        string `json:"username"`
	Password        string `json:"password"` // 脱敏处理
	DisplayName     string `json:"display_name"`
	Role            int    `json:"role"`
	Status          int    `json:"status"`
	Email           string `json:"email,omitempty"`
	Quota           int    `json:"quota"`
	UsedQuota       int    `json:"used_quota"`
	RequestCount    int    `json:"request_count"`
	Group           string `json:"group"`
	AffCode         string `json:"aff_code,omitempty"`
	AffCount        int    `json:"aff_count"`
	AffQuota        int    `json:"aff_quota"`
	AffHistoryQuota int    `json:"aff_history_quota"`
	InviterId       int    `json:"inviter_id"`
	Setting         string `json:"setting,omitempty"`
	Remark          string `json:"remark,omitempty"`
}

// TokenBackup 令牌备份结构
type TokenBackup struct {
	Id                 int     `json:"id"`
	UserId             int     `json:"user_id"`
	Key                string  `json:"key"` // 脱敏处理
	Status             int     `json:"status"`
	Name               string  `json:"name"`
	CreatedTime        int64   `json:"created_time"`
	AccessedTime       int64   `json:"accessed_time"`
	ExpiredTime        int64   `json:"expired_time"`
	RemainQuota        int     `json:"remain_quota"`
	UnlimitedQuota     bool    `json:"unlimited_quota"`
	ModelLimitsEnabled bool    `json:"model_limits_enabled"`
	ModelLimits        string  `json:"model_limits,omitempty"`
	AllowIps           *string `json:"allow_ips,omitempty"`
	UsedQuota          int     `json:"used_quota"`
	Group              string  `json:"group"`
	CrossGroupRetry    bool    `json:"cross_group_retry"`
}

// OptionBackup 配置备份结构
type OptionBackup struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// PrefillGroupBackup 预填充组备份结构
type PrefillGroupBackup struct {
	Id          int             `json:"id"`
	Name        string          `json:"name"`
	Type        string          `json:"type"`
	Items       json.RawMessage `json:"items"`
	Description string          `json:"description,omitempty"`
	CreatedTime int64           `json:"created_time"`
	UpdatedTime int64           `json:"updated_time"`
}

// ExportRequest 导出请求
type ExportRequest struct {
	IncludeSensitive bool     `json:"include_sensitive"`
	Tables           []string `json:"tables"`
}

// ImportRequest 导入请求参数
type ImportRequest struct {
	ConflictStrategy string `form:"conflict_strategy" json:"conflict_strategy"` // skip 或 overwrite
	DryRun           bool   `form:"dry_run" json:"dry_run"`
}

// ImportResult 导入结果
type ImportResult struct {
	Table    string `json:"table"`
	Total    int    `json:"total"`
	Created  int    `json:"created"`
	Updated  int    `json:"updated"`
	Skipped  int    `json:"skipped"`
	Failed   int    `json:"failed"`
	Errors   []string `json:"errors,omitempty"`
}

// 敏感配置项关键字列表
var sensitiveOptionKeys = []string{
	"Token", "Secret", "Key", "Password", "Credential",
	"SMTPToken", "SMTPAccount", "GitHubClientSecret",
	"TelegramBotToken", "EpayKey", "StripeApiSecret",
	"StripeWebhookSecret", "CreemApiKey", "CreemWebhookSecret",
}

// isSensitiveOption 检查配置项是否为敏感项
func isSensitiveOption(key string) bool {
	for _, sensitive := range sensitiveOptionKeys {
		if strings.Contains(key, sensitive) {
			return true
		}
	}
	return false
}

// ExportBackup 导出备份
func ExportBackup(c *gin.Context) {
	var req ExportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		// 如果没有请求体，使用默认值
		req.Tables = []string{"channels", "users", "tokens", "options", "prefill_groups"}
		req.IncludeSensitive = false
	}

	// 如果没有指定表，导出所有
	if len(req.Tables) == 0 {
		req.Tables = []string{"channels", "users", "tokens", "options", "prefill_groups"}
	}

	// 获取当前用户信息
	username := c.GetString("username")
	if username == "" {
		username = "admin"
	}

	backup := BackupFile{
		Meta: BackupMeta{
			Version:   "1.0",
			CreatedAt: time.Now().Unix(),
			CreatedBy: username,
		},
		Data: BackupData{},
	}

	// 导出各表数据
	for _, table := range req.Tables {
		switch table {
		case "channels":
			channels, err := exportChannels(req.IncludeSensitive)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"success": false,
					"message": "导出渠道失败: " + err.Error(),
				})
				return
			}
			backup.Data.Channels = channels

		case "users":
			users, err := exportUsers(req.IncludeSensitive)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"success": false,
					"message": "导出用户失败: " + err.Error(),
				})
				return
			}
			backup.Data.Users = users

		case "tokens":
			tokens, err := exportTokens(req.IncludeSensitive)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"success": false,
					"message": "导出令牌失败: " + err.Error(),
				})
				return
			}
			backup.Data.Tokens = tokens

		case "options":
			options, err := exportOptions(req.IncludeSensitive)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"success": false,
					"message": "导出配置失败: " + err.Error(),
				})
				return
			}
			backup.Data.Options = options

		case "prefill_groups":
			groups, err := exportPrefillGroups()
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"success": false,
					"message": "导出预填充组失败: " + err.Error(),
				})
				return
			}
			backup.Data.PrefillGroups = groups
		}
	}

	// 返回成功响应
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "导出成功",
		"data":    backup,
	})
}

// exportChannels 导出渠道数据
func exportChannels(includeSensitive bool) ([]ChannelBackup, error) {
	channels, err := model.GetAllChannels(0, 0, true, true)
	if err != nil {
		return nil, err
	}

	result := make([]ChannelBackup, 0, len(channels))
	for _, ch := range channels {
		backup := ChannelBackup{
			Id:                 ch.Id,
			Type:               ch.Type,
			OpenAIOrganization: ch.OpenAIOrganization,
			TestModel:          ch.TestModel,
			Status:             ch.Status,
			Name:               ch.Name,
			Weight:             ch.Weight,
			CreatedTime:        ch.CreatedTime,
			BaseURL:            ch.BaseURL,
			Other:              ch.Other,
			Models:             ch.Models,
			Group:              ch.Group,
			ModelMapping:       ch.ModelMapping,
			StatusCodeMapping:  ch.StatusCodeMapping,
			Priority:           ch.Priority,
			AutoBan:            ch.AutoBan,
			OtherInfo:          ch.OtherInfo,
			Tag:                ch.Tag,
			Setting:            ch.Setting,
			ParamOverride:      ch.ParamOverride,
			HeaderOverride:     ch.HeaderOverride,
			Remark:             ch.Remark,
			ChannelInfo:        ch.ChannelInfo,
			OtherSettings:      ch.OtherSettings,
		}

		if includeSensitive {
			backup.Key = ch.Key
		} else {
			backup.Key = "[REDACTED]"
		}

		result = append(result, backup)
	}

	return result, nil
}

// exportUsers 导出用户数据
func exportUsers(includeSensitive bool) ([]UserBackup, error) {
	var users []*model.User
	err := model.DB.Find(&users).Error
	if err != nil {
		return nil, err
	}

	result := make([]UserBackup, 0, len(users))
	for _, u := range users {
		backup := UserBackup{
			Id:              u.Id,
			Username:        u.Username,
			DisplayName:     u.DisplayName,
			Role:            u.Role,
			Status:          u.Status,
			Email:           u.Email,
			Quota:           u.Quota,
			UsedQuota:       u.UsedQuota,
			RequestCount:    u.RequestCount,
			Group:           u.Group,
			AffCode:         u.AffCode,
			AffCount:        u.AffCount,
			AffQuota:        u.AffQuota,
			AffHistoryQuota: u.AffHistoryQuota,
			InviterId:       u.InviterId,
			Setting:         u.Setting,
			Remark:          u.Remark,
		}

		if includeSensitive {
			backup.Password = u.Password
		} else {
			backup.Password = "[REDACTED]"
		}

		result = append(result, backup)
	}

	return result, nil
}

// exportTokens 导出令牌数据
func exportTokens(includeSensitive bool) ([]TokenBackup, error) {
	var tokens []*model.Token
	err := model.DB.Find(&tokens).Error
	if err != nil {
		return nil, err
	}

	result := make([]TokenBackup, 0, len(tokens))
	for _, t := range tokens {
		backup := TokenBackup{
			Id:                 t.Id,
			UserId:             t.UserId,
			Status:             t.Status,
			Name:               t.Name,
			CreatedTime:        t.CreatedTime,
			AccessedTime:       t.AccessedTime,
			ExpiredTime:        t.ExpiredTime,
			RemainQuota:        t.RemainQuota,
			UnlimitedQuota:     t.UnlimitedQuota,
			ModelLimitsEnabled: t.ModelLimitsEnabled,
			ModelLimits:        t.ModelLimits,
			AllowIps:           t.AllowIps,
			UsedQuota:          t.UsedQuota,
			Group:              t.Group,
			CrossGroupRetry:    t.CrossGroupRetry,
		}

		if includeSensitive {
			backup.Key = t.Key
		} else {
			backup.Key = "[REDACTED]"
		}

		result = append(result, backup)
	}

	return result, nil
}

// exportOptions 导出配置数据
func exportOptions(includeSensitive bool) ([]OptionBackup, error) {
	options, err := model.AllOption()
	if err != nil {
		return nil, err
	}

	result := make([]OptionBackup, 0, len(options))
	for _, opt := range options {
		// 跳过敏感配置项
		if !includeSensitive && isSensitiveOption(opt.Key) {
			continue
		}

		result = append(result, OptionBackup{
			Key:   opt.Key,
			Value: opt.Value,
		})
	}

	return result, nil
}

// exportPrefillGroups 导出预填充组数据
func exportPrefillGroups() ([]PrefillGroupBackup, error) {
	groups, err := model.GetAllPrefillGroups("")
	if err != nil {
		return nil, err
	}

	result := make([]PrefillGroupBackup, 0, len(groups))
	for _, g := range groups {
		result = append(result, PrefillGroupBackup{
			Id:          g.Id,
			Name:        g.Name,
			Type:        g.Type,
			Items:       json.RawMessage(g.Items),
			Description: g.Description,
			CreatedTime: g.CreatedTime,
			UpdatedTime: g.UpdatedTime,
		})
	}

	return result, nil
}

// ImportBackup 导入备份
func ImportBackup(c *gin.Context) {
	// 解析请求参数
	conflictStrategy := c.DefaultQuery("conflict_strategy", "skip")
	dryRun := c.DefaultQuery("dry_run", "false") == "true"

	if conflictStrategy != "skip" && conflictStrategy != "overwrite" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "无效的冲突策略，必须是 skip 或 overwrite",
		})
		return
	}

	// 获取上传的文件
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "请上传备份文件",
		})
		return
	}

	// 打开文件
	f, err := file.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "无法打开文件: " + err.Error(),
		})
		return
	}
	defer f.Close()

	// 读取文件内容
	data, err := io.ReadAll(f)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "无法读取文件: " + err.Error(),
		})
		return
	}

	// 解析 JSON
	var backup BackupFile
	if err := json.Unmarshal(data, &backup); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "无效的备份文件格式: " + err.Error(),
		})
		return
	}

	// 验证版本
	if backup.Meta.Version == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "备份文件缺少版本信息",
		})
		return
	}

	results := make([]ImportResult, 0)

	// 开始事务
	tx := model.DB.Begin()
	if tx.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "无法开始事务: " + tx.Error.Error(),
		})
		return
	}

	// 按顺序导入: Users -> Channels -> Tokens -> Options -> PrefillGroups
	if len(backup.Data.Users) > 0 {
		result := importUsers(tx, backup.Data.Users, conflictStrategy, dryRun)
		results = append(results, result)
		if result.Failed > 0 && !dryRun {
			tx.Rollback()
			c.JSON(http.StatusInternalServerError, gin.H{
				"success": false,
				"message": "导入用户失败",
				"data":    results,
			})
			return
		}
	}

	if len(backup.Data.Channels) > 0 {
		result := importChannels(tx, backup.Data.Channels, conflictStrategy, dryRun)
		results = append(results, result)
		if result.Failed > 0 && !dryRun {
			tx.Rollback()
			c.JSON(http.StatusInternalServerError, gin.H{
				"success": false,
				"message": "导入渠道失败",
				"data":    results,
			})
			return
		}
	}

	if len(backup.Data.Tokens) > 0 {
		result := importTokens(tx, backup.Data.Tokens, conflictStrategy, dryRun)
		results = append(results, result)
		if result.Failed > 0 && !dryRun {
			tx.Rollback()
			c.JSON(http.StatusInternalServerError, gin.H{
				"success": false,
				"message": "导入令牌失败",
				"data":    results,
			})
			return
		}
	}

	if len(backup.Data.Options) > 0 {
		result := importOptions(tx, backup.Data.Options, conflictStrategy, dryRun)
		results = append(results, result)
		if result.Failed > 0 && !dryRun {
			tx.Rollback()
			c.JSON(http.StatusInternalServerError, gin.H{
				"success": false,
				"message": "导入配置失败",
				"data":    results,
			})
			return
		}
	}

	if len(backup.Data.PrefillGroups) > 0 {
		result := importPrefillGroups(tx, backup.Data.PrefillGroups, conflictStrategy, dryRun)
		results = append(results, result)
		if result.Failed > 0 && !dryRun {
			tx.Rollback()
			c.JSON(http.StatusInternalServerError, gin.H{
				"success": false,
				"message": "导入预填充组失败",
				"data":    results,
			})
			return
		}
	}

	// 提交或回滚事务
	if dryRun {
		tx.Rollback()
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "预览模式，未实际导入数据",
			"data":    results,
		})
		return
	}

	if err := tx.Commit().Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "提交事务失败: " + err.Error(),
		})
		return
	}

	// 导入渠道后重建 abilities
	if len(backup.Data.Channels) > 0 {
		if _, _, err := model.FixAbility(); err != nil {
			common.SysLog("重建 abilities 失败: " + err.Error())
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "导入成功",
		"data":    results,
	})
}

// importUsers 导入用户
func importUsers(tx *gorm.DB, users []UserBackup, strategy string, dryRun bool) ImportResult {
	result := ImportResult{
		Table:  "users",
		Total:  len(users),
		Errors: make([]string, 0),
	}

	for _, u := range users {
		// 跳过脱敏的密码
		if u.Password == "[REDACTED]" {
			result.Skipped++
			continue
		}

		// 查找是否有匹配的现有记录（按 ID、Username、AffCode 任一匹配）
		// 使用 Unscoped 包含软删除的记录
		var existingUser *model.User = nil
		var matchType string = ""

		// 按 ID 检查
		var userById model.User
		if err := tx.Unscoped().Where("id = ?", u.Id).First(&userById).Error; err == nil {
			existingUser = &userById
			matchType = "id"
		}

		// 如果 ID 没匹配，按 Username 检查
		if existingUser == nil {
			var userByUsername model.User
			if err := tx.Unscoped().Where("username = ?", u.Username).First(&userByUsername).Error; err == nil {
				existingUser = &userByUsername
				matchType = "username"
			}
		}

		// 如果还没匹配，按 AffCode 检查
		if existingUser == nil && u.AffCode != "" {
			var userByAffCode model.User
			if err := tx.Unscoped().Where("aff_code = ?", u.AffCode).First(&userByAffCode).Error; err == nil {
				existingUser = &userByAffCode
				matchType = "aff_code"
			}
		}

		if existingUser != nil {
			// 找到匹配的记录
			if strategy == "skip" {
				result.Skipped++
				continue
			}
			// overwrite - 更新已存在的记录
			if !dryRun {
				// 检查更新是否会与其他记录冲突（使用 Unscoped 包含软删除记录）
				// 如果 username 不同，检查新 username 是否已被其他用户使用
				if existingUser.Username != u.Username {
					var conflictUser model.User
					if err := tx.Unscoped().Where("username = ? AND id != ?", u.Username, existingUser.Id).First(&conflictUser).Error; err == nil {
						result.Failed++
						result.Errors = append(result.Errors, fmt.Sprintf("更新用户 %d 失败: username '%s' 已被用户 %d 使用", u.Id, u.Username, conflictUser.Id))
						continue
					}
				}
				// 如果 aff_code 不同且不为空，检查新 aff_code 是否已被其他用户使用
				if u.AffCode != "" && existingUser.AffCode != u.AffCode {
					var conflictUser model.User
					if err := tx.Unscoped().Where("aff_code = ? AND id != ?", u.AffCode, existingUser.Id).First(&conflictUser).Error; err == nil {
						result.Failed++
						result.Errors = append(result.Errors, fmt.Sprintf("更新用户 %d 失败: aff_code '%s' 已被用户 %d 使用", u.Id, u.AffCode, conflictUser.Id))
						continue
					}
				}

				existingUser.Username = u.Username
				existingUser.DisplayName = u.DisplayName
				existingUser.Role = u.Role
				existingUser.Status = u.Status
				existingUser.Email = u.Email
				existingUser.Quota = u.Quota
				existingUser.UsedQuota = u.UsedQuota
				existingUser.RequestCount = u.RequestCount
				existingUser.Group = u.Group
				existingUser.AffCode = u.AffCode
				existingUser.AffCount = u.AffCount
				existingUser.AffQuota = u.AffQuota
				existingUser.AffHistoryQuota = u.AffHistoryQuota
				existingUser.InviterId = u.InviterId
				existingUser.Setting = u.Setting
				existingUser.Remark = u.Remark
				if u.Password != "[REDACTED]" {
					existingUser.Password = u.Password
				}
				// 如果是软删除的记录，先恢复它
				if existingUser.DeletedAt.Valid {
					existingUser.DeletedAt = gorm.DeletedAt{}
				}
				// 使用 Unscoped 保存，确保能更新软删除的记录
				if err := tx.Unscoped().Save(existingUser).Error; err != nil {
					result.Failed++
					result.Errors = append(result.Errors, fmt.Sprintf("更新用户 (%s匹配) %d 失败: %s", matchType, u.Id, err.Error()))
					continue
				}
			}
			result.Updated++
		} else {
			// 没有匹配的记录，创建新用户
			if !dryRun {
				// 检查 aff_code 是否已被使用（包括软删除的记录），如果是则生成新的
				affCode := u.AffCode
				if affCode != "" {
					var conflictUser model.User
					if err := tx.Unscoped().Where("aff_code = ?", affCode).First(&conflictUser).Error; err == nil {
						// aff_code 已被使用，生成新的
						affCode = common.GetRandomString(4)
						// 确保新生成的也不冲突
						for i := 0; i < 10; i++ {
							var check model.User
							if err := tx.Unscoped().Where("aff_code = ?", affCode).First(&check).Error; err != nil {
								break // 没找到，可以使用
							}
							affCode = common.GetRandomString(4)
						}
					}
				}

				newUser := model.User{
					Username:        u.Username,
					Password:        u.Password,
					DisplayName:     u.DisplayName,
					Role:            u.Role,
					Status:          u.Status,
					Email:           u.Email,
					Quota:           u.Quota,
					UsedQuota:       u.UsedQuota,
					RequestCount:    u.RequestCount,
					Group:           u.Group,
					AffCode:         affCode,
					AffCount:        u.AffCount,
					AffQuota:        u.AffQuota,
					AffHistoryQuota: u.AffHistoryQuota,
					InviterId:       u.InviterId,
					Setting:         u.Setting,
					Remark:          u.Remark,
				}
				if err := tx.Create(&newUser).Error; err != nil {
					result.Failed++
					result.Errors = append(result.Errors, fmt.Sprintf("创建用户 %d 失败: %s", u.Id, err.Error()))
					continue
				}
			}
			result.Created++
		}
	}

	return result
}

// importChannels 导入渠道
func importChannels(tx *gorm.DB, channels []ChannelBackup, strategy string, dryRun bool) ImportResult {
	result := ImportResult{
		Table:  "channels",
		Total:  len(channels),
		Errors: make([]string, 0),
	}

	for _, ch := range channels {
		// 跳过脱敏的 key
		if ch.Key == "[REDACTED]" {
			result.Skipped++
			continue
		}

		// 检查是否存在
		var existing model.Channel
		err := tx.Where("id = ?", ch.Id).First(&existing).Error

		if err == nil {
			// 记录存在
			if strategy == "skip" {
				result.Skipped++
				continue
			}
			// overwrite
			if !dryRun {
				existing.Type = ch.Type
				existing.Key = ch.Key
				existing.OpenAIOrganization = ch.OpenAIOrganization
				existing.TestModel = ch.TestModel
				existing.Status = ch.Status
				existing.Name = ch.Name
				existing.Weight = ch.Weight
				existing.CreatedTime = ch.CreatedTime
				existing.BaseURL = ch.BaseURL
				existing.Other = ch.Other
				existing.Models = ch.Models
				existing.Group = ch.Group
				existing.ModelMapping = ch.ModelMapping
				existing.StatusCodeMapping = ch.StatusCodeMapping
				existing.Priority = ch.Priority
				existing.AutoBan = ch.AutoBan
				existing.OtherInfo = ch.OtherInfo
				existing.Tag = ch.Tag
				existing.Setting = ch.Setting
				existing.ParamOverride = ch.ParamOverride
				existing.HeaderOverride = ch.HeaderOverride
				existing.Remark = ch.Remark
				existing.ChannelInfo = ch.ChannelInfo
				existing.OtherSettings = ch.OtherSettings
				if err := tx.Save(&existing).Error; err != nil {
					result.Failed++
					result.Errors = append(result.Errors, fmt.Sprintf("更新渠道 %d 失败: %s", ch.Id, err.Error()))
					continue
				}
			}
			result.Updated++
		} else if err == gorm.ErrRecordNotFound {
			// 记录不存在，创建新记录（不指定 ID，让数据库自动生成）
			if !dryRun {
				newChannel := model.Channel{
					Type:               ch.Type,
					Key:                ch.Key,
					OpenAIOrganization: ch.OpenAIOrganization,
					TestModel:          ch.TestModel,
					Status:             ch.Status,
					Name:               ch.Name,
					Weight:             ch.Weight,
					CreatedTime:        ch.CreatedTime,
					BaseURL:            ch.BaseURL,
					Other:              ch.Other,
					Models:             ch.Models,
					Group:              ch.Group,
					ModelMapping:       ch.ModelMapping,
					StatusCodeMapping:  ch.StatusCodeMapping,
					Priority:           ch.Priority,
					AutoBan:            ch.AutoBan,
					OtherInfo:          ch.OtherInfo,
					Tag:                ch.Tag,
					Setting:            ch.Setting,
					ParamOverride:      ch.ParamOverride,
					HeaderOverride:     ch.HeaderOverride,
					Remark:             ch.Remark,
					ChannelInfo:        ch.ChannelInfo,
					OtherSettings:      ch.OtherSettings,
				}
				if err := tx.Create(&newChannel).Error; err != nil {
					result.Failed++
					result.Errors = append(result.Errors, fmt.Sprintf("创建渠道 %d 失败: %s", ch.Id, err.Error()))
					continue
				}
			}
			result.Created++
		} else {
			// 查询出错
			result.Failed++
			result.Errors = append(result.Errors, fmt.Sprintf("查询渠道 %d 失败: %s", ch.Id, err.Error()))
		}
	}

	return result
}

// importTokens 导入令牌
func importTokens(tx *gorm.DB, tokens []TokenBackup, strategy string, dryRun bool) ImportResult {
	result := ImportResult{
		Table:  "tokens",
		Total:  len(tokens),
		Errors: make([]string, 0),
	}

	// 用于跟踪本次导入中已处理的 key，避免重复
	processedKeys := make(map[string]bool)

	for _, t := range tokens {
		// 跳过脱敏的 key
		if t.Key == "[REDACTED]" {
			result.Skipped++
			continue
		}

		// 清理 key 中可能的空格（char 类型可能有填充）
		cleanKey := strings.TrimSpace(t.Key)

		// 检查本次导入中是否已处理过这个 key
		if processedKeys[cleanKey] {
			result.Skipped++
			continue
		}

		// 先按 ID 检查是否存在（使用 Unscoped 包含软删除的记录）
		var existingById model.Token
		errById := tx.Unscoped().Where("id = ?", t.Id).First(&existingById).Error

		// 再按 Key 检查是否存在（Key 有唯一约束）
		// 使用 Unscoped 包含软删除的记录，因为唯一索引包含所有记录
		var existingByKey model.Token
		errByKey := tx.Unscoped().Where("key = ? OR key LIKE ?", cleanKey, cleanKey+"%").First(&existingByKey).Error

		if errById == nil {
			// 按 ID 找到记录（可能是软删除的）
			if strategy == "skip" {
				result.Skipped++
				processedKeys[cleanKey] = true
				continue
			}
			// overwrite
			if !dryRun {
				existingById.UserId = t.UserId
				existingById.Key = cleanKey
				existingById.Status = t.Status
				existingById.Name = t.Name
				existingById.CreatedTime = t.CreatedTime
				existingById.AccessedTime = t.AccessedTime
				existingById.ExpiredTime = t.ExpiredTime
				existingById.RemainQuota = t.RemainQuota
				existingById.UnlimitedQuota = t.UnlimitedQuota
				existingById.ModelLimitsEnabled = t.ModelLimitsEnabled
				existingById.ModelLimits = t.ModelLimits
				existingById.AllowIps = t.AllowIps
				existingById.UsedQuota = t.UsedQuota
				existingById.Group = t.Group
				existingById.CrossGroupRetry = t.CrossGroupRetry
				existingById.DeletedAt = gorm.DeletedAt{} // 恢复软删除的记录
				if err := tx.Unscoped().Save(&existingById).Error; err != nil {
					result.Failed++
					result.Errors = append(result.Errors, fmt.Sprintf("更新令牌 %d 失败: %s", t.Id, err.Error()))
					continue
				}
			}
			result.Updated++
			processedKeys[cleanKey] = true
		} else if errById == gorm.ErrRecordNotFound {
			// 按 ID 找不到，检查是否按 Key 能找到
			if errByKey == nil {
				// Key 已存在但 ID 不同（可能是软删除的）
				if strategy == "skip" {
					result.Skipped++
					processedKeys[cleanKey] = true
					continue
				}
				// overwrite - 更新已存在的记录
				if !dryRun {
					existingByKey.UserId = t.UserId
					existingByKey.Status = t.Status
					existingByKey.Name = t.Name
					existingByKey.CreatedTime = t.CreatedTime
					existingByKey.AccessedTime = t.AccessedTime
					existingByKey.ExpiredTime = t.ExpiredTime
					existingByKey.RemainQuota = t.RemainQuota
					existingByKey.UnlimitedQuota = t.UnlimitedQuota
					existingByKey.ModelLimitsEnabled = t.ModelLimitsEnabled
					existingByKey.ModelLimits = t.ModelLimits
					existingByKey.AllowIps = t.AllowIps
					existingByKey.UsedQuota = t.UsedQuota
					existingByKey.Group = t.Group
					existingByKey.CrossGroupRetry = t.CrossGroupRetry
					existingByKey.DeletedAt = gorm.DeletedAt{} // 恢复软删除的记录
					if err := tx.Unscoped().Save(&existingByKey).Error; err != nil {
						result.Failed++
						result.Errors = append(result.Errors, fmt.Sprintf("更新令牌 (key匹配) 失败: %s", err.Error()))
						continue
					}
				}
				result.Updated++
				processedKeys[cleanKey] = true
			} else if errByKey == gorm.ErrRecordNotFound {
				// ID 和 Key 都不存在，创建新记录（不指定 ID，让数据库自动生成）
				if !dryRun {
					newToken := model.Token{
						UserId:             t.UserId,
						Key:                cleanKey,
						Status:             t.Status,
						Name:               t.Name,
						CreatedTime:        t.CreatedTime,
						AccessedTime:       t.AccessedTime,
						ExpiredTime:        t.ExpiredTime,
						RemainQuota:        t.RemainQuota,
						UnlimitedQuota:     t.UnlimitedQuota,
						ModelLimitsEnabled: t.ModelLimitsEnabled,
						ModelLimits:        t.ModelLimits,
						AllowIps:           t.AllowIps,
						UsedQuota:          t.UsedQuota,
						Group:              t.Group,
						CrossGroupRetry:    t.CrossGroupRetry,
					}
					if err := tx.Create(&newToken).Error; err != nil {
						result.Failed++
						result.Errors = append(result.Errors, fmt.Sprintf("创建令牌 %d 失败: %s", t.Id, err.Error()))
						continue
					}
				}
				result.Created++
				processedKeys[cleanKey] = true
			} else {
				// 按 Key 查询出错
				result.Failed++
				result.Errors = append(result.Errors, fmt.Sprintf("查询令牌 key 失败: %s", errByKey.Error()))
			}
		} else {
			// 按 ID 查询出错
			result.Failed++
			result.Errors = append(result.Errors, fmt.Sprintf("查询令牌 %d 失败: %s", t.Id, errById.Error()))
		}
	}

	return result
}

// importOptions 导入配置
func importOptions(tx *gorm.DB, options []OptionBackup, strategy string, dryRun bool) ImportResult {
	result := ImportResult{
		Table:  "options",
		Total:  len(options),
		Errors: make([]string, 0),
	}

	for _, opt := range options {
		// 检查是否存在
		var existing model.Option
		err := tx.Where("key = ?", opt.Key).First(&existing).Error

		if err == nil {
			// 记录存在
			if strategy == "skip" {
				result.Skipped++
				continue
			}
			// overwrite
			if !dryRun {
				existing.Value = opt.Value
				if err := tx.Save(&existing).Error; err != nil {
					result.Failed++
					result.Errors = append(result.Errors, fmt.Sprintf("更新配置 %s 失败: %s", opt.Key, err.Error()))
					continue
				}
			}
			result.Updated++
		} else if err == gorm.ErrRecordNotFound {
			// 记录不存在，创建新记录
			if !dryRun {
				newOption := model.Option{
					Key:   opt.Key,
					Value: opt.Value,
				}
				if err := tx.Create(&newOption).Error; err != nil {
					result.Failed++
					result.Errors = append(result.Errors, fmt.Sprintf("创建配置 %s 失败: %s", opt.Key, err.Error()))
					continue
				}
			}
			result.Created++
		} else {
			// 查询出错
			result.Failed++
			result.Errors = append(result.Errors, fmt.Sprintf("查询配置 %s 失败: %s", opt.Key, err.Error()))
		}
	}

	return result
}

// importPrefillGroups 导入预填充组
func importPrefillGroups(tx *gorm.DB, groups []PrefillGroupBackup, strategy string, dryRun bool) ImportResult {
	result := ImportResult{
		Table:  "prefill_groups",
		Total:  len(groups),
		Errors: make([]string, 0),
	}

	for _, g := range groups {
		// 先按 ID 检查是否存在
		var existingById model.PrefillGroup
		errById := tx.Where("id = ?", g.Id).First(&existingById).Error

		// 再按 Name 检查是否存在（Name 有唯一约束）
		var existingByName model.PrefillGroup
		errByName := tx.Where("name = ?", g.Name).First(&existingByName).Error

		if errById == nil {
			// 按 ID 找到记录
			if strategy == "skip" {
				result.Skipped++
				continue
			}
			// overwrite
			if !dryRun {
				existingById.Name = g.Name
				existingById.Type = g.Type
				existingById.Items = model.JSONValue(g.Items)
				existingById.Description = g.Description
				existingById.CreatedTime = g.CreatedTime
				existingById.UpdatedTime = g.UpdatedTime
				if err := tx.Save(&existingById).Error; err != nil {
					result.Failed++
					result.Errors = append(result.Errors, fmt.Sprintf("更新预填充组 %d 失败: %s", g.Id, err.Error()))
					continue
				}
			}
			result.Updated++
		} else if errById == gorm.ErrRecordNotFound {
			// 按 ID 找不到，检查是否按 Name 能找到
			if errByName == nil {
				// Name 已存在但 ID 不同
				if strategy == "skip" {
					result.Skipped++
					continue
				}
				// overwrite - 更新已存在的记录
				if !dryRun {
					existingByName.Type = g.Type
					existingByName.Items = model.JSONValue(g.Items)
					existingByName.Description = g.Description
					existingByName.CreatedTime = g.CreatedTime
					existingByName.UpdatedTime = g.UpdatedTime
					if err := tx.Save(&existingByName).Error; err != nil {
						result.Failed++
						result.Errors = append(result.Errors, fmt.Sprintf("更新预填充组 (name匹配) %s 失败: %s", g.Name, err.Error()))
						continue
					}
				}
				result.Updated++
			} else if errByName == gorm.ErrRecordNotFound {
				// ID 和 Name 都不存在，创建新记录（不指定 ID，让数据库自动生成）
				if !dryRun {
					newGroup := model.PrefillGroup{
						Name:        g.Name,
						Type:        g.Type,
						Items:       model.JSONValue(g.Items),
						Description: g.Description,
						CreatedTime: g.CreatedTime,
						UpdatedTime: g.UpdatedTime,
					}
					if err := tx.Create(&newGroup).Error; err != nil {
						result.Failed++
						result.Errors = append(result.Errors, fmt.Sprintf("创建预填充组 %d 失败: %s", g.Id, err.Error()))
						continue
					}
				}
				result.Created++
			} else {
				// 按 Name 查询出错
				result.Failed++
				result.Errors = append(result.Errors, fmt.Sprintf("查询预填充组 name %s 失败: %s", g.Name, errByName.Error()))
			}
		} else {
			// 按 ID 查询出错
			result.Failed++
			result.Errors = append(result.Errors, fmt.Sprintf("查询预填充组 %d 失败: %s", g.Id, errById.Error()))
		}
	}

	return result
}
