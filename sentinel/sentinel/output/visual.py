"""Visual report generation."""

import html
from pathlib import Path
from typing import Optional
from datetime import datetime

from .report import ThreatReport
from ..providers.base import ThreatLevel
from ..logging import get_logger

logger = get_logger(__name__)


HTML_TEMPLATE = '''<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Sentinel Threat Report - {file_name}</title>
    <style>
        * {{ margin: 0; padding: 0; box-sizing: border-box; }}
        body {{ 
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
            background: #1a1a2e; 
            color: #eee;
            padding: 20px;
        }}
        .container {{ max-width: 800px; margin: 0 auto; }}
        .header {{ 
            background: linear-gradient(135deg, #16213e 0%, #0f3460 100%);
            padding: 24px;
            border-radius: 12px;
            margin-bottom: 20px;
        }}
        .header h1 {{ font-size: 24px; margin-bottom: 8px; }}
        .header .subtitle {{ color: #888; font-size: 14px; }}
        .threat-badge {{
            display: inline-block;
            padding: 8px 16px;
            border-radius: 20px;
            font-weight: bold;
            font-size: 14px;
            text-transform: uppercase;
        }}
        .threat-clean {{ background: #2d5a27; color: #90EE90; }}
        .threat-suspicious {{ background: #5a4a27; color: #FFD700; }}
        .threat-malicious {{ background: #5a2727; color: #FF6B6B; }}
        .threat-critical {{ background: #5a1a1a; color: #FF0000; }}
        .threat-unknown {{ background: #3a3a3a; color: #888; }}
        .score-bar {{
            background: #333;
            border-radius: 10px;
            height: 24px;
            margin: 16px 0;
            overflow: hidden;
        }}
        .score-fill {{
            height: 100%;
            border-radius: 10px;
            display: flex;
            align-items: center;
            padding-left: 12px;
            font-weight: bold;
            font-size: 12px;
        }}
        .section {{
            background: #16213e;
            padding: 20px;
            border-radius: 12px;
            margin-bottom: 16px;
        }}
        .section h2 {{
            font-size: 16px;
            color: #4fc3f7;
            margin-bottom: 16px;
            border-bottom: 1px solid #333;
            padding-bottom: 8px;
        }}
        .verdicts {{ display: grid; gap: 12px; }}
        .verdict {{
            background: #1a1a2e;
            padding: 12px;
            border-radius: 8px;
            border-left: 4px solid #333;
        }}
        .verdict.malicious {{ border-left-color: #FF6B6B; }}
        .verdict.suspicious {{ border-left-color: #FFD700; }}
        .verdict.clean {{ border-left-color: #90EE90; }}
        .verdict-header {{
            display: flex;
            justify-content: space-between;
            align-items: center;
            margin-bottom: 8px;
        }}
        .verdict-name {{ font-weight: bold; }}
        .verdict-score {{ 
            font-family: monospace;
            background: #333;
            padding: 2px 8px;
            border-radius: 4px;
        }}
        .findings {{ list-style: none; }}
        .findings li {{
            padding: 8px 12px;
            background: #1a1a2e;
            margin-bottom: 4px;
            border-radius: 4px;
            font-size: 14px;
        }}
        .indicators {{
            display: flex;
            flex-wrap: wrap;
            gap: 8px;
        }}
        .indicator {{
            background: #333;
            padding: 4px 12px;
            border-radius: 16px;
            font-size: 12px;
            font-family: monospace;
        }}
        .indicator.ip {{ border: 1px solid #FF6B6B; }}
        .indicator.url {{ border: 1px solid #4fc3f7; }}
        .indicator.domain {{ border: 1px solid #FFD700; }}
        .meta-grid {{
            display: grid;
            grid-template-columns: repeat(2, 1fr);
            gap: 12px;
        }}
        .meta-item {{
            background: #1a1a2e;
            padding: 12px;
            border-radius: 8px;
        }}
        .meta-label {{ color: #888; font-size: 12px; margin-bottom: 4px; }}
        .meta-value {{ font-family: monospace; word-break: break-all; }}
        .footer {{
            text-align: center;
            color: #666;
            font-size: 12px;
            margin-top: 20px;
            padding: 12px;
        }}
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>SENTINEL THREAT REPORT</h1>
            <div class="subtitle">{timestamp}</div>
        </div>

        <div class="section">
            <div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 16px;">
                <div>
                    <div style="font-size: 20px; font-weight: bold;">{file_name}</div>
                    <div style="color: #888; font-size: 12px;">{file_type} | {file_size}</div>
                </div>
                <span class="threat-badge threat-{threat_level_class}">{threat_level}</span>
            </div>
            
            <div class="score-bar">
                <div class="score-fill" style="width: {score_percent}%; background: {score_color};">
                    {score_display}
                </div>
            </div>
            
            <div style="text-align: center; color: #888; font-size: 14px;">
                Confidence: {confidence_percent}% | Analysis Time: {analysis_time}
            </div>
        </div>

        <div class="section">
            <h2>PROVIDER VERDICTS</h2>
            <div class="verdicts">
                {verdicts_html}
            </div>
        </div>

        {findings_section}

        {indicators_section}

        <div class="section">
            <h2>FILE DETAILS</h2>
            <div class="meta-grid">
                <div class="meta-item">
                    <div class="meta-label">SHA-256</div>
                    <div class="meta-value">{file_hash}</div>
                </div>
                <div class="meta-item">
                    <div class="meta-label">File ID</div>
                    <div class="meta-value">{file_id}</div>
                </div>
            </div>
        </div>

        <div class="footer">
            Generated by Sentinel Detection Engine | {timestamp}
        </div>
    </div>
</body>
</html>'''


class VisualReportGenerator:
    """Generate visual HTML reports."""
    
    THREAT_COLORS = {
        ThreatLevel.CLEAN: "#2d5a27",
        ThreatLevel.SUSPICIOUS: "#5a4a27",
        ThreatLevel.MALICIOUS: "#5a2727",
        ThreatLevel.CRITICAL: "#5a1a1a",
        ThreatLevel.UNKNOWN: "#3a3a3a",
    }
    
    SCORE_COLORS = {
        "low": "#90EE90",
        "medium": "#FFD700",
        "high": "#FF6B6B",
    }
    
    def __init__(self, output_dir: Optional[str] = None):
        self.output_dir = Path(output_dir) if output_dir else Path("./reports")
        self.output_dir.mkdir(parents=True, exist_ok=True)
    
    def generate(self, report: ThreatReport) -> str:
        """Generate HTML report."""
        threat_class = report.threat_level.value.lower()
        
        if report.final_score >= 0.7:
            score_color = self.SCORE_COLORS["high"]
        elif report.final_score >= 0.4:
            score_color = self.SCORE_COLORS["medium"]
        else:
            score_color = self.SCORE_COLORS["low"]
        
        verdicts_html = self._generate_verdicts_html(report.provider_verdicts)
        findings_html = self._generate_findings_html(report.findings)
        indicators_html = self._generate_indicators_html(report.indicators)
        
        file_size = self._format_size(report.file_size)
        timestamp = datetime.fromtimestamp(report.timestamp).strftime("%Y-%m-%d %H:%M:%S UTC")
        
        html_content = HTML_TEMPLATE.format(
            file_name=html.escape(report.file_name),
            file_type=html.escape(report.file_type.upper()),
            file_size=file_size,
            file_hash=report.file_hash,
            file_id=report.file_id,
            threat_level=report.threat_level.value.upper(),
            threat_level_class=threat_class,
            score_percent=int(report.final_score * 100),
            score_display=f"{report.final_score:.0%}",
            score_color=score_color,
            confidence_percent=int(report.confidence * 100),
            analysis_time=f"{report.analysis_time_ms:.0f}ms",
            timestamp=timestamp,
            verdicts_html=verdicts_html,
            findings_section=findings_html,
            indicators_section=indicators_html,
        )
        
        return html_content
    
    def save(self, report: ThreatReport, filename: Optional[str] = None) -> Path:
        """Save HTML report to file."""
        html_content = self.generate(report)
        
        if filename is None:
            filename = f"report_{report.file_id}.html"
        
        output_path = self.output_dir / filename
        output_path.write_text(html_content, encoding="utf-8")
        
        logger.info(f"Saved report to {output_path}")
        return output_path
    
    def _generate_verdicts_html(self, verdicts) -> str:
        """Generate HTML for provider verdicts."""
        if not verdicts:
            return '<div class="verdict">No provider verdicts available</div>'
        
        html_parts = []
        for v in verdicts:
            threat_class = v.threat_level.value.lower()
            findings_list = "".join(
                f'<div style="font-size: 12px; color: #888; margin-top: 4px;">- {html.escape(f)}</div>'
                for f in v.findings[:3]
            )
            
            html_parts.append(f'''
                <div class="verdict {threat_class}">
                    <div class="verdict-header">
                        <span class="verdict-name">{html.escape(v.provider_name)}</span>
                        <span class="verdict-score">{v.score:.0%}</span>
                    </div>
                    {findings_list}
                </div>
            ''')
        
        return "\n".join(html_parts)
    
    def _generate_findings_html(self, findings) -> str:
        """Generate HTML for findings section."""
        if not findings:
            return ""
        
        items = "".join(
            f'<li>{html.escape(f)}</li>'
            for f in findings[:15]
        )
        
        return f'''
            <div class="section">
                <h2>FINDINGS</h2>
                <ul class="findings">{items}</ul>
            </div>
        '''
    
    def _generate_indicators_html(self, indicators) -> str:
        """Generate HTML for indicators section."""
        if not indicators:
            return ""
        
        items = []
        for ind in indicators[:20]:
            ind_type = ind.get("type", "unknown")
            ind_value = html.escape(ind.get("value", ""))
            items.append(f'<span class="indicator {ind_type}">{ind_value}</span>')
        
        return f'''
            <div class="section">
                <h2>INDICATORS</h2>
                <div class="indicators">{"".join(items)}</div>
            </div>
        '''
    
    def _format_size(self, size: int) -> str:
        """Format file size for display."""
        for unit in ["B", "KB", "MB", "GB"]:
            if size < 1024:
                return f"{size:.1f} {unit}"
            size /= 1024
        return f"{size:.1f} TB"
