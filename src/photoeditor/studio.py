"""Small desktop config editor using the same engine via an isolated worker process."""
import json
import os
from pathlib import Path
import sys
import tempfile

import yaml


def launch(source,dest,config_path):
    try:
        from PySide6.QtCore import Qt,QProcess,QProcessEnvironment,QTimer
        from PySide6.QtGui import QPixmap
        from PySide6.QtWidgets import (QApplication,QMainWindow,QWidget,QHBoxLayout,QVBoxLayout,
            QSplitter,QPlainTextEdit,QPushButton,QLabel,QListWidget,QMessageBox,QScrollArea)
    except ImportError:
        raise ValueError('Install editor dependencies with scripts/setup.sh --studio')
    from .config import load
    class Studio(QMainWindow):
        def __init__(self):
            super().__init__()
            self.setWindowTitle('PhotoEditor Studio — config & collection preview')
            self.resize(1500,940)
            self.process=None;self.revision=0;self.active_revision=-1
            self.temporary=None
            root=QWidget();self.setCentralWidget(root);layout=QVBoxLayout(root)
            self.status=QLabel('Edit YAML, select a photo, then Preview. Proxy is not a 100% detail check.')
            layout.addWidget(self.status)
            split=QSplitter();layout.addWidget(split,1)
            self.files=QListWidget();split.addWidget(self.files)
            cfg=load(config_path)
            from .jobs import scan
            for i in scan(source,cfg):self.files.addItem(i['relative'])
            images=QWidget();panels=QHBoxLayout(images)
            self.before=QLabel('Before');self.after=QLabel('After')
            for label in [self.before,self.after]:
                label.setAlignment(Qt.AlignmentFlag.AlignCenter);label.setMinimumSize(300,250)
                panels.addWidget(label)
            split.addWidget(images)
            self.text=QPlainTextEdit(config_path.read_text());split.addWidget(self.text)
            split.setSizes([220,880,400])
            self.log=QPlainTextEdit();self.log.setReadOnly(True);self.log.setMaximumHeight(170);layout.addWidget(self.log)
            bar=QHBoxLayout();layout.addLayout(bar)
            for title,fn in [('Validate',self.validate),('Save config',self.save),('Preview selected',self.preview),
                             ('Render collection',self.batch),('Cancel',self.cancel)]:
                button=QPushButton(title);button.clicked.connect(fn);bar.addWidget(button)
            self.files.currentTextChanged.connect(self.show_before)
            self.text.textChanged.connect(self.changed)
            if self.files.count():self.files.setCurrentRow(0)
        def changed(self):
            self.revision+=1
            self.status.setText('Unsaved changes — preview/result from an older revision is not current.')
        def snapshot(self):
            dest.mkdir(parents=True,exist_ok=True)
            # Named file persisted until subprocess finishes, no shared hardcoded filename.
            f=tempfile.NamedTemporaryFile(mode='w',suffix='.yaml',prefix='.studio-',dir=dest,delete=False)
            with f:f.write(self.text.toPlainText())
            path=Path(f.name)
            try:load(path)
            except Exception:path.unlink(missing_ok=True);raise
            return path
        def validate(self):
            try:
                p=self.snapshot();p.unlink();self.status.setText('Configuration valid')
                return True
            except Exception as exc:QMessageBox.warning(self,'Invalid config',str(exc));return False
        def save(self):
            if not self.validate():return
            temp=config_path.with_suffix('.yaml.tmp');temp.write_text(self.text.toPlainText());os.replace(temp,config_path)
            self.status.setText('Configuration saved')
        def show_before(self,relative):
            if relative:self.show_pixmap(self.before,source/relative)
        def show_pixmap(self,label,path):
            pix=QPixmap(str(path))
            label.setPixmap(pix.scaled(label.size(),Qt.AspectRatioMode.KeepAspectRatio,Qt.TransformationMode.SmoothTransformation))
        def preview(self):
            item=self.files.currentItem()
            if item:self.start(['preview','--image',item.text()])
        def batch(self):self.start(['render'])
        def start(self,args):
            if self.process is not None:
                QMessageBox.information(self,'Busy','Cancel or wait for the current job.');return
            try:self.temporary=self.snapshot()
            except Exception as exc:QMessageBox.warning(self,'Invalid config',str(exc));return
            self.active_revision=self.revision
            self.process=QProcess(self)
            env=QProcessEnvironment.systemEnvironment()
            env.insert('PHOTO_INPUT_DIR',str(source));env.insert('PHOTO_OUTPUT_DIR',str(dest))
            env.insert('PHOTO_CONFIG_FILE',str(self.temporary));env.insert('PYTHONUNBUFFERED','1')
            self.process.setProcessEnvironment(env)
            self.process.setProgram(sys.executable);self.process.setArguments(['-m','photoeditor.cli']+args)
            self.buffer=''
            self.process.readyReadStandardOutput.connect(self.read_output)
            self.process.readyReadStandardError.connect(lambda:self.log.appendPlainText(bytes(self.process.readAllStandardError()).decode(errors='replace')))
            self.process.finished.connect(self.finished)
            self.process.start();self.status.setText('Processing…')
        def read_output(self):
            self.buffer+=bytes(self.process.readAllStandardOutput()).decode(errors='replace')
            while '\n' in self.buffer:
                line,self.buffer=self.buffer.split('\n',1);self.log.appendPlainText(line)
                try:event=json.loads(line)
                except ValueError:continue
                if event.get('event')=='preview_ready' and self.active_revision==self.revision:
                    self.show_pixmap(self.after,event['path'])
        def finished(self,code,status):
            self.read_output()
            self.status.setText(f'Finished, exit {code}. '+('Config changed; request a new preview.' if self.active_revision!=self.revision else 'Inspect results; studio quality requires visual review.'))
            if self.temporary:self.temporary.unlink(missing_ok=True)
            self.process.deleteLater();self.process=None
        def cancel(self):
            if self.process:
                import signal
                os.kill(self.process.processId(),signal.SIGINT)
        def closeEvent(self,event):
            if self.process:
                self.cancel();event.ignore();return
            event.accept()
    app=QApplication.instance() or QApplication(sys.argv)
    window=Studio();window.show()
    return app.exec()
