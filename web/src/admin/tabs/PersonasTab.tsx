import { memo, useState } from 'react';
import { classNames } from '../../shared/lib/classNames';
import { pretty } from '../../shared/lib/data';
import type { PersonaAdmin } from '../hooks/usePersonaAdmin';
import { matchesQuery } from '../lib/adminData';
import { Field } from '../components/Field';
import { ListPane } from '../components/ListPane';

export type PersonasTabProps = Pick<PersonaAdmin,
  'personas' |
  'selectedPersona' |
  'personaDraft' |
  'progressDraft' |
  'progressDraftJSON' |
  'progressDraftError' |
  'progressDefaults' |
  'reloadPersonas' |
  'selectPersona' |
  'patchPersonaDraft' |
  'patchProgressDraftJSON' |
  'newPersona' |
  'submitPersona' |
  'deleteSelectedPersona'
> & {
  activePersona: string;
};

export default memo(function PersonasTab({
  personas,
  selectedPersona,
  personaDraft,
  progressDraft,
  progressDraftJSON,
  progressDraftError,
  progressDefaults,
  reloadPersonas,
  selectPersona,
  patchPersonaDraft,
  patchProgressDraftJSON,
  newPersona,
  submitPersona,
  deleteSelectedPersona,
  activePersona,
}: PersonasTabProps) {
  const [query, setQuery] = useState('');
  const visiblePersonas = personas.filter(persona => matchesQuery(query, persona.key, persona.name, persona.description, persona.tone));

  return (
    <div className="admin-split">
      <ListPane title="Persona" count={`${personas.length} 个 Persona · 当前：${activePersona || '无'}`} searchID="persona-search" searchValue={query} searchLabel="Persona" onSearch={setQuery} onNew={newPersona} onReload={reloadPersonas}>
        {visiblePersonas.map(persona => <button className={classNames('item', selectedPersona === persona.key && 'active')} type="button" key={persona.key} onClick={() => selectPersona(String(persona.key))}><span className="item-title"><span className="item-name">{persona.name || persona.key}</span><span className={classNames('badge', persona.key === activePersona && 'ok')}>{persona.key === activePersona ? '当前' : 'Persona'}</span></span><span className="item-meta">{persona.key} · {String(persona.description || persona.tone || '')}</span></button>)}
      </ListPane>
      <section className="detail-pane">
        <form className="section" id="persona-form" onSubmit={submitPersona}>
          <div className="hero"><div><h2 id="persona-title">{personaDraft.name || personaDraft.key || '新 Persona'}</h2><div className="meta" id="persona-meta">{personaDraft.key || 'Persona 文件以 ID 作为键'}</div></div><button className="btn primary" id="save-persona" type="submit">保存 Persona</button></div>
          <div className="grid">
            <Field id="persona-key" label="Key" value={String(personaDraft.key || '')} onChange={value => patchPersonaDraft('key', value)} readOnly={!!selectedPersona} mono />
            <Field id="persona-name" label="名称" value={String(personaDraft.name || '')} onChange={value => patchPersonaDraft('name', value)} />
            <Field id="persona-description" label="描述" value={String(personaDraft.description || '')} onChange={value => patchPersonaDraft('description', value)} />
            <Field id="persona-tone" label="语气" value={String(personaDraft.tone || '')} onChange={value => patchPersonaDraft('tone', value)} />
          </div>
          <div className="field"><label htmlFor="persona-greeting">问候语</label><textarea id="persona-greeting" value={String(personaDraft.greeting || '')} onChange={event => patchPersonaDraft('greeting', event.target.value)} /></div>
          <div className="field"><label htmlFor="persona-system">系统提示词</label><textarea id="persona-system" value={String(personaDraft.system_prompt || '')} onChange={event => patchPersonaDraft('system_prompt', event.target.value)} /></div>
          <div className="grid">
            <Field id="persona-quirks" label="特点" value={Array.isArray(personaDraft.quirks) ? personaDraft.quirks.join(', ') : ''} onChange={value => patchPersonaDraft('quirks', value.split(',').map(item => item.trim()).filter(Boolean))} />
            <div className="field"><label htmlFor="persona-progress">工作进度短语 JSON</label><textarea id="persona-progress" value={progressDraftJSON} onChange={event => patchProgressDraftJSON(event.target.value)} />{progressDraftError && <div className="field-error">{progressDraftError}</div>}</div>
          </div>
          <pre className="code" id="persona-progress-defaults">{pretty({ current: progressDraft, defaults: progressDefaults })}</pre>
          <div className="actions foot"><button className="btn danger" id="delete-persona" type="button" disabled={!selectedPersona} onClick={deleteSelectedPersona}>删除</button></div>
        </form>
      </section>
    </div>
  );
});
