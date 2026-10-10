package models

type PlayoutStatus string

const (
	PlayoutInactiveStatus PlayoutStatus = "INACTIVE"
	PlayoutLIVEStatus     PlayoutStatus = "LIVE"
	PlayoutPrepareStatus  PlayoutStatus = "PREPARE"
	PlayoutFailureStatus  PlayoutStatus = "FAILURE"
)
