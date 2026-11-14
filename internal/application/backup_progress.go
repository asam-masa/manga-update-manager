package application

type BackupStage string

const (
	BackupStageWaiting       BackupStage = "waiting"
	BackupStagePreparing     BackupStage = "preparing"
	BackupStageDatabase      BackupStage = "database"
	BackupStageDatabaseCheck BackupStage = "database_check"
	BackupStageImages        BackupStage = "images"
	BackupStageMetadata      BackupStage = "metadata"
	BackupStageZIP           BackupStage = "zip"
	BackupStagePublishing    BackupStage = "publishing"
	BackupStageCleanup       BackupStage = "cleanup"
)

// BackupProgress is a synchronous observation, not a control hook. The owner
// binds it to one execution ID. It must be short, non-blocking, and do no I/O or
// saving/control work. Call only outside state locks; nil means unobserved.
type BackupProgress func(BackupStage)

func (p BackupProgress) Notify(stage BackupStage) {
	if p != nil {
		p(stage)
	}
}
