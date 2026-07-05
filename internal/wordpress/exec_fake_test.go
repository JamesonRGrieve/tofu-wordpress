// SPDX-License-Identifier: AGPL-3.0-or-later

package wordpress

// fakeExec is a scripted Executor for hermetic tests: it records every command
// it is asked to run and returns a pre-scripted (stdout, err) per call index.
// This is the ONLY way the apply/relocation logic is exercised in unit tests —
// no device is ever contacted.
type fakeResp struct {
	out string
	err error
}

type fakeExec struct {
	responses []fakeResp
	calls     []string
	idx       int
}

func (f *fakeExec) Run(cmd string, _ []byte) ([]byte, error) {
	f.calls = append(f.calls, cmd)
	var r fakeResp
	if f.idx < len(f.responses) {
		r = f.responses[f.idx]
	}
	f.idx++
	return []byte(r.out), r.err
}
