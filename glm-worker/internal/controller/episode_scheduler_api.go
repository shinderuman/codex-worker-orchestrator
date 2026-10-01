package controller

func (s *Store) ResolveFindingWithProjectAuthority(
	findingID string,
	decision FindingDecision,
) (FindingDispositionResult, error) {
	result, err := s.ResolveFinding(findingID, decision)
	if err != nil || result.Episode == nil {
		return result, err
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

func (s *Store) ScheduleEpisodeWithProjectAuthority(
	episodeID string,
	revision uint64,
) (EpisodeScheduleResult, error) {
	legacy, err := s.ScheduleEpisode(episodeID, revision)
	if err != nil {
		return EpisodeScheduleResult{}, err
	}
	return s.scheduleEpisodeAgainstProject(legacy.Episode)
}
