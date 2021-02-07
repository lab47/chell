package evt

type EVTNode interface {
	evtNode()
}

type Statements struct {
	Statements []EVTNode
}

type SystemNode struct {
	Arguments []string
	Dir       string
}

type SetRoot struct {
	Dir string
}

type ChangeDir struct {
	Dir  string
	Body EVTNode
}

type MakeDir struct {
	Dir string
}

type Shell struct {
	Code string
}

type Patch struct {
	Patch string
}

func (s *Statements) evtNode() {}
func (s *SystemNode) evtNode() {}
func (s *SetRoot) evtNode()    {}
func (s *ChangeDir) evtNode()  {}
func (s *MakeDir) evtNode()    {}
func (s *Shell) evtNode()      {}
func (s *Patch) evtNode()      {}
