// SPDX-License-Identifier: Apache-2.0

// Package iec104go encodes IEC 60870-5-104 APCI frames and companion-standard
// ASDUs, supervises independent TCP sessions, and provides client/server and
// initiating procedure helpers. Station application policy is deliberately
// separate. Decoders preserve information-element flags and own returned bytes.
package iec104go
