export const state = {
  mediaData: [],
  providerOrder: [],
  providerEnabled: {},
  providerFields: {},
  providerConfigured: {},
  providerDefs: [],
  isScanning: false,
  expandedIds: new Set(),
  expandedSeasonIds: new Set(),
  pickerFileId: null,
  pickerCandidates: [],
  imdbSearchTimer: null,
  imdbTargetMediaId: null,
  workers: 5,
  autoScanInterval: '0',
  tmdbApiKey: '',
  fileCache: new Map(),  // mediaId → apiFile[]
  settingsDirty: false,
  fetchingMediaIds: new Set(),    // mediaId — fetchMedia in progress
  fetchingSeasonKeys: new Set(),  // "mediaId-season" — fetchSeason in progress
  fetchingFileIds: new Set(),     // fileId — fetchFile in progress
  autoScanActive: false,          // true while a poller-triggered scan is in progress
  nfoArt: [],                     // artItem[] for the open NFO modal
  nfoArtIndex: 0,                 // current carousel slide
  nfoArtMediaId: null             // media id whose art the carousel serves
};
