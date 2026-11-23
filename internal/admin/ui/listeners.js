export function listenerLabels(listeners) {
  const ports = listeners.tuner_ports;
  const groups = [];
  for (const port of ports) {
    const last = groups[groups.length - 1];
    if (last && port === last[1] + 1) last[1] = port;
    else groups.push([port, port]);
  }
  return {
    range: listeners.port_range ? listeners.port_range.replace('-', '–') : 'Individual ports (legacy configuration)',
    admin: listeners.admin_port ? String(listeners.admin_port) : 'Shared with the primary tuner' + (ports.length ? ' on ' + ports[0] : ''),
    tuners: groups.map(([first,last]) => first === last ? String(first) : first + '–' + last).join(', '),
    capacity: listeners.available_slots + ' of ' + ports.length + ' tuner slots available.',
  };
}

export function slotHelp(listeners) {
  if (listeners.available_slots > 0) return 'Each saved tuner uses one slot, even when disabled.';
  return 'Choose an existing tuner, or remove an unused one in Tuners to free a slot.';
}

export function tunerSlotSummary(listeners) {
  const {capacity, tuners} = listenerLabels(listeners);
  return capacity + ' Plex uses TCP ' + tuners + '. ' + slotHelp(listeners);
}
