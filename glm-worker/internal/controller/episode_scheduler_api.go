package controller

func (s *Store) ResolveFindingWithProjectAuthority(
	findingID string,
	decision FindingDecision,
) (FindingDispositionResult, error) {
	result, err := s.ResolveFinding(findingID, decision)
	if err != nil {
		return FindingDispositionResult{}, err
	}
	if err := s.restoreCommittedBlockingEpisode(&result); err != nil {
		return FindingDispositionResult{}, err
	}
	if result.Episode == nil {
		return result, nil
	}
	schedule, err := s.scheduleEpisodeAgainstProject(*result.Episode)
	if err != nil {
		return FindingDispositionResult{}, err
	}
	result.NextTaskRef = schedule.NextTaskRef
	result.Reason = schedule.Reason
	if schedule.Intent == FindingIntentNoRunnable {
		result.Intent = FindingIntentNoRunnable
	}
	return result, nil
}

func (s *Store) restoreCommittedBlockingEpisode(result *FindingDispositionResult) error {
	if result.Episode != nil || result.Disposition == nil ||
		result.Disposition.Kind != FindingDispositionIndependentBlocking {
		return nil
	}
	disposition := result.Disposition
	if disposition.EpisodeID == "" || disposition.EpisodeRevision == 0 {
		return nil
	}
	revision, err := s.LoadEpisodeRevision(disposition.EpisodeID, disposition.EpisodeRevision)
	if err != nil {
		return err
	}
	result.Episode = &revision
	result.Intent = FindingIntentOpenBlockerEpisode
	if revision.Revision > 1 {
		result.Intent = FindingIntentReplanBlockerEpisode
	}
	return nil
}

func (s *Store) ScheduleEpisodeWithProjectAuthority(
	episodeID string,
	revision uint64,
) (EpisodeScheduleResult, error) {
	legacy, err := s.scheduleEpisode(episodeID, revision)
	if err != nil {
		return EpisodeScheduleResult{}, err
	}
	return s.scheduleEpisodeAgainstProject(legacy.Episode)
}
