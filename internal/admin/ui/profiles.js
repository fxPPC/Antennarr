// Keep every profile mutation explicit. An empty selection must never fall back
// to the primary tuner and accidentally replace its channels.
export function profilePath(path, profileID) {
  if (!profileID) throw new Error('Choose a tuner first.');
  const [base, query = ''] = path.split('?');
  const params = new URLSearchParams(query);
  params.set('profile_id', profileID);
  return base + '?' + params;
}

export function profileChannels(channels, profileID) {
  return channels.filter(channel => channel.profile_id === profileID);
}

export function profileLabel(profiles, profileID) {
  return profiles.find(profile => profile.id === profileID)?.name || 'Unavailable tuner';
}
