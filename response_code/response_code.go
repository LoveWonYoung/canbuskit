package response_code

var NrcMap = map[byte]string{
	0x00: "PositiveResponse",
	0x10: "GeneralReject",
	0x11: "ServiceNotSupported",
	0x12: "SubFunctionNotSupported",
	0x13: "IncorrectMessageLengthOrInvalidFormat",
	0x14: "ResponseTooLong",
	0x21: "BusyRepeatRequest",
	0x22: "ConditionsNotCorrect",
	0x24: "RequestSequenceError",
	0x25: "NoResponseFromSubnetComponent",
	0x26: "FailurePreventsExecutionOfRequestedAction",
	0x31: "RequestOutOfRange",
	0x33: "SecurityAccessDenied",
	0x34: "AuthenticationRequired",
	0x35: "InvalidKey",
	0x36: "ExceedNumberOfAttempts",
	0x37: "RequiredTimeDelayNotExpired",
	0x38: "SecureDataTransmissionRequired", // Note: Also used as an offset
	0x39: "SecureDataTransmissionNotAllowed",
	0x3A: "SecureDataVerificationFailed",
	0x50: "CertificateVerificationFailed_InvalidTimePeriod",
	0x51: "CertificateVerificationFailed_InvalidSignature",
	0x52: "CertificateVerificationFailed_InvalidChainOfTrust",
	0x53: "CertificateVerificationFailed_InvalidType",
	0x54: "CertificateVerificationFailed_InvalidFormat",
	0x55: "CertificateVerificationFailed_InvalidContent",
	0x56: "CertificateVerificationFailed_InvalidScope",
	0x57: "CertificateVerificationFailed_InvalidCertificate",
	0x58: "OwnershipVerificationFailed",
	0x59: "ChallengeCalculationFailed",
	0x5A: "SettingAccessRightsFailed",
	0x5B: "SessionKeyCreationDerivationFailed",
	0x5C: "ConfigurationDataUsageFailed",
	0x5D: "DeAuthenticationFailed",
	0x70: "UploadDownloadNotAccepted",
	0x71: "TransferDataSuspended",
	0x72: "GeneralProgrammingFailure",
	0x73: "WrongBlockSequenceCounter",
	0x78: "RequestCorrectlyReceived_ResponsePending",
	0x7E: "SubFunctionNotSupportedInActiveSession",
	0x7F: "ServiceNotSupportedInActiveSession",
	0x81: "RpmTooHigh",
	0x82: "RpmTooLow",
	0x83: "EngineIsRunning",
	0x84: "EngineIsNotRunning",
	0x85: "EngineRunTimeTooLow",
	0x86: "TemperatureTooHigh",
	0x87: "TemperatureTooLow",
	0x88: "VehicleSpeedTooHigh",
	0x89: "VehicleSpeedTooLow",
	0x8A: "ThrottlePedalTooHigh",
	0x8B: "ThrottlePedalTooLow",
	0x8C: "TransmissionRangeNotInNeutral",
	0x8D: "TransmissionRangeNotInGear",
	0x8F: "BrakeSwitchNotClosed",
	0x90: "ShifterLeverNotInPark",
	0x91: "TorqueConverterClutchLocked",
	0x92: "VoltageTooHigh",
	0x93: "VoltageTooLow",
	0x94: "ResourceTemporarilyNotAvailable",

	// ISO-15764 definitions with 0x38 offset
	//0x38 + 0: "GeneralSecurityViolation", // This will overwrite the key 0x38
	//0x38 + 1: "SecuredModeRequested",
	//0x38 + 2: "InsufficientProtection",
	0x38 + 3: "TerminationWithSignatureRequested",
	0x38 + 4: "AccessDenied",
	0x38 + 5: "VersionNotSupported",
	0x38 + 6: "SecuredLinkNotSupported",
	0x38 + 7: "CertificateNotAvailable",
	0x38 + 8: "AuditTrailInformationNotAvailable",
}

// GetNrcString takes a numeric NRC code and returns its string representation.
// If the code is not found, it returns "Unknown NRC".
func GetNrcString(nrc byte) string {
	if str, ok := NrcMap[nrc]; ok {
		return str
	}
	return "Unknown NRC"
}
