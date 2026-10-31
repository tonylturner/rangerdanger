package server

func euLab3Check(name string, ok bool, detail string) ValidationCheck {
	if ok {
		return checkPass(name, detail)
	}
	return checkFail(name, "missing attempt-window evidence: "+detail)
}
