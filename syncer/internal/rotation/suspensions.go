package rotation

// Verified full-day closures. New unexplained gaps still stop publication.
// Issuer notices (also recorded in docs/deployments/four-etf-rotation.md):
// 159915: https://pdf.dfcfw.com/pdf/H2_AN202102081459963456_1.pdf
// 513100: https://static.cninfo.com.cn/finalpage/2022-01-12/1212149066.PDF
func verifiedSuspension(code, date string) bool {
	return code == "159915.SZ" && date == "20210208" || code == "513100.SH" && date == "20220113"
}
