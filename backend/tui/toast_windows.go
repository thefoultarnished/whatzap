//go:build windows

package main

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

func showToast(title, body string) {
	title = strings.TrimSpace(title)
	body = strings.TrimSpace(body)
	if title == "" {
		title = "WhatZap"
	}
	if body == "" {
		body = "New activity"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	_ = exec.CommandContext(ctx, "powershell", "-NoProfile", "-Command", windowsToastScript(title, body)).Run()
}

func windowsToastScript(title, body string) string {
	return strings.Join([]string{
		"$ErrorActionPreference = 'Stop'",
		"[Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime] | Out-Null",
		"[Windows.Data.Xml.Dom.XmlDocument, Windows.Data.Xml.Dom.XmlDocument, ContentType = WindowsRuntime] | Out-Null",
		"try { " +
			"$xml = New-Object Windows.Data.Xml.Dom.XmlDocument; " +
			"$xml.LoadXml(\"<toast><visual><binding template='ToastGeneric'><text>" + xmlEscape(title) + "</text><text>" + xmlEscape(body) + "</text></binding></visual></toast>\"); " +
			"$toast = [Windows.UI.Notifications.ToastNotification]::new($xml); " +
			"$notifier = [Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier('WhatZap'); " +
			"$notifier.Show($toast) " +
		"} catch { " +
			"Add-Type -AssemblyName System.Windows.Forms; " +
			"Add-Type -AssemblyName System.Drawing; " +
			"$n = New-Object System.Windows.Forms.NotifyIcon; " +
			"$n.Icon = [System.Drawing.SystemIcons]::Information; " +
			"$n.BalloonTipIcon = [System.Windows.Forms.ToolTipIcon]::Info; " +
			"$n.BalloonTipTitle = '" + psSingleQuote(title) + "'; " +
			"$n.BalloonTipText = '" + psSingleQuote(body) + "'; " +
			"$n.Visible = $true; " +
			"$n.ShowBalloonTip(5000); " +
			"Start-Sleep -Milliseconds 5500; " +
			"$n.Dispose() " +
		"}",
	}, "; ")
}

func xmlEscape(s string) string {
	replacer := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
		"'", "&apos;",
	)
	return replacer.Replace(s)
}

func psSingleQuote(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}
