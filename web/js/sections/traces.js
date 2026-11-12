// Slowest Statements Traced section (fork): the traces the analysis built
// for the slowest statements (see trace/ and docs/trace.md). The section
// lists them; a row opens the trace in the query modal: where the time went,
// the explained findings, the query with its parameters, the replay, the
// plan, the temp files of the backend and of its parallel workers, and the
// timeline. The texts computed by the analysis come in the language of the
// traces (data.traces_lang); the labels below follow it.

import { fmt, esc, escAttr, escForJsAttr, fmtMs, fmtMsLong, truncQuery, buildNoDataMessage } from '../utils.js';
import { fmtBytes } from '../format.js';
import { analysisData } from '../state.js';
import { explainDaliboFlow } from './modals.js';

const LABELS = {
    en: {
        section: 'Slowest Statements Traced',
        sectionTip: 'Each of the slowest statements followed through the logs: its own auto_explain plan, its parameters, the temp files of the backend and of its parallel workers, and what the backend logged while it ran. Click a row for the trace.',
        noData: '<code>log_min_duration_statement</code> (and <code>auto_explain</code> for plans)',
        filtered: 'the slowest of the whole log, kept when they ran in the selected range',
        duration: 'Duration', started: 'Started', ended: 'Ended', query: 'Query', time: 'Where the time went',
        tempFiles: 'Temp files', findings: 'Findings', workers: 'Parallel workers', statement: 'Statement',
        trace: 'Trace', queryDetails: 'Query details', executor: 'Executor', after: 'After the executor',
        parameters: 'Parameters', prepared: 'Prepared text found in DETAIL: prepare.',
        replay: 'Replay', replayNote: 'EXPLAIN without ANALYZE only plans the query. EXPLAIN (ANALYZE, BUFFERS) runs it for real: same cost as in production.',
        plan: 'Plan', planMeta: (at, ms) => 'auto_explain at ' + at + ', ' + ms + ' in the executor',
        rebuilt: 'The log had flattened this plan onto one line: its indentation is rebuilt.',
        why: 'Why', impact: 'Impact here', check: 'To check (read-only)', proposal: 'To propose',
        ofTime: 'of the time',
        other: 'Other Plans on This Backend', otherMeta: 'not this statement',
        otherNote: 'Logged by the same backend during the window, but their Query Text is another statement.',
        timeCol: 'Time', role: 'Role', size: 'Size', path: 'Path', kind: 'Kind', event: 'Event',
        files: n => n + ' files', stored: '← stored result, freed after sending', timeline: 'Timeline',
        tracedRuns: 'Traced Runs', tracedTip: 'The slowest runs of this query followed through the logs: plan, parameters, temp files, timeline.',
        verdict: { client: 'Sending to the client', outside: 'Outside the executor', executor: 'In the executor', unknown: 'No plan logged' },
        copy: 'Copy', copied: 'Copied!', visualize: 'Visualize',
    },
    fr: {
        section: 'Requêtes les plus lentes, tracées',
        sectionTip: 'Chacune des requêtes les plus lentes suivie dans les journaux : son propre plan auto_explain, ses paramètres, les fichiers temporaires du backend et de ses workers parallèles, et ce que le backend a journalisé pendant qu\'elle tournait. Cliquer sur une ligne pour la trace.',
        noData: '<code>log_min_duration_statement</code> (et <code>auto_explain</code> pour les plans)',
        filtered: 'les plus lentes de tout le journal, gardées si elles ont tourné dans la plage choisie',
        duration: 'Durée', started: 'Début', ended: 'Fin', query: 'Requête', time: 'Où est passé le temps',
        tempFiles: 'Fichiers temp.', findings: 'Constats', workers: 'Workers parallèles', statement: 'Instruction',
        trace: 'Trace', queryDetails: 'Détail de la requête', executor: 'Exécuteur', after: 'Après l\'exécuteur',
        parameters: 'Paramètres', prepared: 'Texte préparé trouvé dans DETAIL: prepare.',
        replay: 'Rejouer', replayNote: 'EXPLAIN sans ANALYZE ne fait que planifier la requête. EXPLAIN (ANALYZE, BUFFERS) l\'exécute vraiment : même coût qu\'en production.',
        plan: 'Plan', planMeta: (at, ms) => 'auto_explain à ' + at + ', ' + ms + ' dans l\'exécuteur',
        rebuilt: 'Le journal avait mis ce plan sur une seule ligne : son indentation est reconstituée.',
        why: 'Pourquoi', impact: 'Impact ici', check: 'À vérifier (lecture seule)', proposal: 'À proposer',
        ofTime: 'du temps',
        other: 'Autres plans sur ce backend', otherMeta: 'pas cette requête',
        otherNote: 'Journalisés par le même backend pendant la fenêtre, mais leur Query Text est une autre requête.',
        timeCol: 'Heure', role: 'Rôle', size: 'Taille', path: 'Chemin', kind: 'Type', event: 'Événement',
        files: n => n + ' fichiers', stored: '← résultat stocké, libéré après l\'envoi', timeline: 'Chronologie',
        tracedRuns: 'Exécutions tracées', tracedTip: 'Les exécutions les plus lentes de cette requête suivies dans les journaux : plan, paramètres, fichiers temporaires, chronologie.',
        verdict: { client: 'Envoi au client', outside: 'Hors de l\'exécuteur', executor: 'Dans l\'exécuteur', unknown: 'Pas de plan journalisé' },
        copy: 'Copier', copied: 'Copié !', visualize: 'Visualiser',
    },
};

function labels(data) {
    return LABELS[(data || analysisData)?.traces_lang] || LABELS.en;
}

// ts turns "2026-11-12T19:57:32" into "2026-11-12 19:57:32".
function ts(s) {
    return String(s || '').replace('T', ' ');
}

function copyButton(text, L) {
    return '<button class="copy-btn-inline" onclick="navigator.clipboard.writeText(\'' + escForJsAttr(text || '') +
        '\');this.textContent=\'' + escForJsAttr(L.copied) + '\';setTimeout(()=>this.textContent=\'' + escForJsAttr(L.copy) + '\',1500)">' + esc(L.copy) + '</button>';
}

function session(t) {
    return [['db', t.database], ['user', t.user], ['app', t.app], ['client', t.client]]
        .filter(([, v]) => v).map(([k, v]) => k + '=' + v).join(', ');
}

// The split bar: executor time against the rest of the statement's duration.
function splitBar(t, L) {
    const s = t.time_split || {};
    if (!s.known || !(t.duration_ms > 0)) {
        return '<div class="tr-split tr-split--unknown" title="' + escAttr(s.explanation || '') + '"><span style="width:100%"></span></div>';
    }
    const exec = Math.max(0, Math.min(100, 100 * s.executor_ms / t.duration_ms));
    return '<div class="tr-split" title="' + escAttr(L.executor + ' ' + fmtMs(s.executor_ms) + ', ' + L.after + ' ' + fmtMs(s.after_ms)) + '">' +
        '<span class="tr-split-exec" style="width:' + exec.toFixed(1) + '%"></span>' +
        '<span class="tr-split-after tr-split-after--' + escAttr(s.verdict) + '" style="width:' + (100 - exec).toFixed(1) + '%"></span></div>';
}

function verdict(t, L) {
    return '<div class="tr-verdict">' + esc(L.verdict[t.time_split?.verdict] || '') + '</div>';
}

export function buildTracesSection(data) {
    const L = labels(data);
    const traces = data.traces || [];
    if (traces.length === 0) {
        return `
            <div class="section" id="traces">
                <div class="section-header">${esc(L.section)}</div>
                <div class="section-body">
                    ${buildNoDataMessage(L.noData)}
                </div>
            </div>
        `;
    }
    const filtered = data._timeFiltered ? '<span class="qd-meta">' + esc(L.filtered) + '</span>' : '';
    return `
        <div class="section" id="traces">
            <div class="section-header">${esc(L.section)}
                <ql-tooltip text="${escAttr(L.sectionTip)}">i</ql-tooltip>
            </div>
            <div class="section-body">
                ${filtered}
                <div class="table-container">
                    <table class="tr-table">
                        <thead><tr>
                            <th class="num">${esc(L.duration)}</th>
                            <th>${esc(L.ended)}</th>
                            <th>PID</th>
                            <th>${esc(L.query)}</th>
                            <th>${esc(L.time)}</th>
                            <th class="num">${esc(L.tempFiles)}</th>
                            <th>${esc(L.findings)}</th>
                        </tr></thead>
                        <tbody>
                            ${traces.map((t, i) => `
                                <tr class="tr-row" onclick="showTraceModal(${i})">
                                    <td class="num">${esc(fmtMs(t.duration_ms))}</td>
                                    <td>${esc(ts(t.end))}</td>
                                    <td>${esc(t.pid)}</td>
                                    <td class="query-cell">${esc(truncQuery(t.query || t.statement || '', 90))}</td>
                                    <td>${splitBar(t, L)}${verdict(t, L)}</td>
                                    <td class="num">${t.temp_files?.length ? esc(fmtBytes(t.temp_bytes)) + ' <span class="qd-meta">(' + fmt(t.temp_files.length) + ')</span>' : '—'}</td>
                                    <td class="tr-main-finding">${t.findings?.length ? esc(truncQuery(t.findings[0].title, 70)) + (t.findings.length > 1 ? ' <span class="qd-meta">+' + fmt(t.findings.length - 1) + '</span>' : '') : '—'}</td>
                                </tr>
                            `).join('')}
                        </tbody>
                    </table>
                </div>
            </div>
        </div>
    `;
}

// tracesForQuery returns the traces of one query (sql_performance id), so
// the query modal can link to them.
export function tracesForQuery(queryId) {
    const traces = analysisData?.traces || [];
    return traces.map((t, i) => ({ t, i })).filter(x => queryId && x.t.query_id === queryId);
}

// buildQueryTracesHTML is the Traced Runs block of the query modal.
export function buildQueryTracesHTML(queryId) {
    const rows = tracesForQuery(queryId);
    if (rows.length === 0) return '';
    const L = labels();
    return '<div class="qd-section"><div class="qd-section-title">' + esc(L.tracedRuns) + ' ' +
        '<ql-tooltip text="' + escAttr(L.tracedTip) + '">i</ql-tooltip></div>' +
        '<div class="table-container"><table class="tr-table"><thead><tr><th class="num">' + esc(L.duration) + '</th><th>' + esc(L.ended) +
        '</th><th>PID</th><th>' + esc(L.time) + '</th><th></th></tr></thead><tbody>' +
        rows.map(({ t, i }) => '<tr class="tr-row" onclick="showTraceModal(' + i + ')"><td class="num">' + esc(fmtMs(t.duration_ms)) + '</td><td>' +
            esc(ts(t.end)) + '</td><td>' + esc(t.pid) + '</td><td>' + splitBar(t, L) + verdict(t, L) +
            '</td><td><button class="btn-visualize">' + esc(L.trace) + '</button></td></tr>').join('') +
        '</tbody></table></div></div>';
}

export function showTraceModal(index) {
    const t = analysisData?.traces?.[index];
    if (!t) return;
    document.getElementById('queryModalBody').innerHTML = buildTraceHTML(t, index);
    document.getElementById('queryModal').open();
}

export function visualizeTracePlan(index) {
    const t = analysisData?.traces?.[index];
    if (!t?.plan?.lines) return;
    explainDaliboFlow(t.plan.lines.join('\n'), t.query, 'trace_' + t.pid);
}

function section(title, body, extra = '') {
    return '<div class="qd-section"><div class="qd-section-title" style="display:flex;justify-content:space-between;align-items:center;gap:4px;">' +
        title + extra + '</div>' + body + '</div>';
}

function buildFinding(f, L) {
    let html = '<li class="tr-finding"><div class="tr-finding-title"><strong>' + esc(f.title) + '</strong>';
    if (f.weight > 0) html += ' <span class="tr-weight">' + esc((100 * f.weight).toFixed(f.weight < 0.1 ? 1 : 0)) + ' % ' + esc(L.ofTime) + '</span>';
    html += '</div>';
    if (f.why) html += '<div class="tr-finding-part"><span class="tr-finding-label">' + esc(L.why) + '</span>' + esc(f.why) + '</div>';
    if (f.impact) html += '<div class="tr-finding-part"><span class="tr-finding-label">' + esc(L.impact) + '</span>' + esc(f.impact) + '</div>';
    for (const c of f.checks || []) {
        html += '<div class="tr-finding-part"><span class="tr-finding-label">' + esc(L.check) + '</span>' + copyButton(c, L) +
            '<pre class="query-detail-sql tr-check">' + esc(c) + '</pre></div>';
    }
    if (f.proposal) html += '<div class="tr-finding-part"><span class="tr-finding-label">' + esc(L.proposal) + '</span>' + esc(f.proposal) + '</div>';
    return html + '</li>';
}

function buildTraceHTML(t, index) {
    const L = labels();
    let html = '';
    const s = t.time_split || {};
    const stat = (label, value) => '<div class="qd-stat"><div class="qd-stat-label">' + esc(label) + '</div><div class="qd-stat-value">' + esc(value) + '</div></div>';
    let head = '<div class="qd-stats">' + stat(L.duration, fmtMsLong(t.duration_ms)) + stat(L.started, ts(t.start)) +
        stat(L.ended, ts(t.end)) + stat('PID', t.pid) + (t.workers?.length ? stat(L.workers, t.workers.join(', ')) : '') + '</div>';
    if (session(t)) head += '<div class="qd-meta" style="margin-top:0.4rem;">' + esc(session(t)) + '</div>';
    head += '<div class="qd-meta" style="margin-top:0.4rem;">' + esc(L.statement) + ' : ' + esc(truncQuery(t.statement || '', 200)) + '</div>';
    const toQuery = t.query_id
        ? '<button class="btn-visualize" style="margin-left:auto;" onclick="showQueryModal(\'' + escForJsAttr(t.query_id) + '\')">' + esc(L.queryDetails) + '</button>'
        : '';
    html += section(esc(L.trace), head, toQuery);

    let split = splitBar(t, L);
    if (s.known) {
        split += '<div class="tr-split-legend"><span><i class="tr-split-exec"></i>' + esc(L.executor) + ' ' + esc(fmtMs(s.executor_ms)) +
            '</span><span><i class="tr-split-after tr-split-after--' + escAttr(s.verdict) + '"></i>' + esc(L.after) + ' ' + esc(fmtMs(s.after_ms)) + '</span></div>';
    }
    split += '<p class="tr-explanation">' + esc(s.explanation || '') + '</p>';
    html += section(esc(L.time), split);

    if (t.findings?.length) {
        html += section(esc(L.findings), '<ul class="tr-findings">' + t.findings.map(f => buildFinding(f, L)).join('') + '</ul>');
    }

    let query = '<div class="query-detail-sql" style="white-space:pre-wrap;">' + esc(t.query || '') + '</div>';
    if (t.parameters) query += '<div class="qd-meta" style="margin-top:0.4rem;">' + esc(L.parameters) + ' : ' + esc(t.parameters) + '</div>';
    if (t.prepared) query += '<div class="qd-meta">' + esc(L.prepared) + '</div>';
    html += section(esc(L.query), query, copyButton(t.query, L));

    html += section(esc(L.replay),
        '<div class="query-detail-sql" style="white-space:pre-wrap;">' + esc(t.replay || '') + '</div>' +
        '<div class="qd-meta" style="margin-top:0.4rem;">' + esc(L.replayNote) + '</div>',
        copyButton(t.replay, L));

    if (t.plan?.lines) {
        const planText = t.plan.lines.join('\n');
        let body = '<div class="query-detail-sql" style="white-space:pre;font-size:0.75rem;overflow-x:auto;">' + esc(planText) + '</div>';
        if (t.plan.rebuilt) body += '<div class="qd-meta">' + esc(L.rebuilt) + '</div>';
        html += section(esc(L.plan) + ' <span class="qd-meta">' + esc(L.planMeta(ts(t.plan.time), fmtMs(t.plan.duration_ms))) + '</span>', body,
            '<span style="margin-left:auto;display:flex;gap:4px;"><button class="btn-visualize" onclick="visualizeTracePlan(' + index + ')">' + esc(L.visualize) + '</button>' + copyButton(planText, L) + '</span>');
    }

    if (t.foreign_plans?.length) {
        html += section(esc(L.other) + ' <span class="qd-meta">' + esc(L.otherMeta) + '</span>',
            '<div class="table-container"><table><thead><tr><th>' + esc(L.timeCol) + '</th><th class="num">' + esc(L.executor) + '</th><th>Query Text</th></tr></thead><tbody>' +
            t.foreign_plans.map(p => '<tr><td>' + esc(ts(p.time)) + '</td><td class="num">' + esc(fmtMs(p.duration_ms)) +
                '</td><td class="query-cell">' + esc(truncQuery(p.query_text || '', 120)) + '</td></tr>').join('') +
            '</tbody></table></div><div class="qd-meta">' + esc(L.otherNote) + '</div>');
    }

    if (t.temp_files?.length) {
        html += section(esc(L.tempFiles) + ' <span class="qd-meta">' + esc(L.files(fmt(t.temp_files.length))) + ', ' + esc(fmtBytes(t.temp_bytes)) + '</span>',
            '<div class="table-container" style="max-height:240px;"><table><thead><tr><th>' + esc(L.timeCol) + '</th><th>PID</th><th>' + esc(L.role) +
            '</th><th class="num">' + esc(L.size) + '</th><th>' + esc(L.path) + '</th></tr></thead><tbody>' +
            t.temp_files.map(f => '<tr' + (f.stored_result ? ' class="tr-stored"' : '') + '><td>' + esc(ts(f.time)) + '</td><td>' + esc(f.pid) +
                '</td><td>' + esc(f.role) + '</td><td class="num">' + esc(fmtBytes(f.bytes)) + '</td><td>' + esc(f.path) +
                (f.stored_result ? ' <span class="qd-meta">' + esc(L.stored) + '</span>' : '') + '</td></tr>').join('') +
            '</tbody></table></div>');
    }

    html += section(esc(L.timeline),
        '<div class="table-container" style="max-height:280px;"><table><thead><tr><th>' + esc(L.timeCol) + '</th><th>PID</th><th>' + esc(L.kind) +
        '</th><th>' + esc(L.event) + '</th></tr></thead><tbody>' +
        (t.events || []).map(ev => '<tr class="tr-ev tr-ev--' + escAttr(ev.kind) + '"><td>' + esc(ts(ev.time)) + '</td><td>' + esc(ev.pid) +
            '</td><td>' + esc(ev.kind) + '</td><td class="query-cell">' + esc(truncQuery(ev.text || '', 160)) + '</td></tr>').join('') +
        '</tbody></table></div>');
    return html;
}
