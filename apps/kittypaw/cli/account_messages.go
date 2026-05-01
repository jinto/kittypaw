package main

import (
	"fmt"
	"strings"
)

const (
	accountCredentialsIntroKo          = "KittyPaw 사용자 계정을 설정합니다.\n계정 ID와 비밀번호를 입력해주세요.\n계정 ID와 비밀번호 정보는 이 컴퓨터에만 저장됩니다."
	accountCredentialsIntroEn          = "Set up a KittyPaw user account.\nEnter an account ID and password to continue.\nYour account ID and password data are stored only on this computer."
	accountCredentialsIntroWithIDKoFmt = "KittyPaw 사용자 계정을 설정합니다: %s\n이 계정의 비밀번호를 입력해주세요.\n계정 ID와 비밀번호 정보는 이 컴퓨터에만 저장됩니다."
	accountCredentialsIntroWithIDEnFmt = "Set up a KittyPaw user account: %s\nEnter a password for this account.\nYour account ID and password data are stored only on this computer."
)

func accountCredentialsIntroMessage(accountID string) string {
	accountID = strings.TrimSpace(accountID)
	if strings.HasPrefix(detectLang(), "ko") {
		if accountID == "" {
			return accountCredentialsIntroKo
		}
		return fmt.Sprintf(accountCredentialsIntroWithIDKoFmt, accountID)
	}

	if accountID == "" {
		return accountCredentialsIntroEn
	}
	return fmt.Sprintf(accountCredentialsIntroWithIDEnFmt, accountID)
}
