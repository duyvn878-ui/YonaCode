import sys
import os
from PyQt5.QtWidgets import (QApplication, QWidget, QVBoxLayout, QPushButton, QLabel, 
                             QFileDialog, QMessageBox, QProgressBar, 
                             QRadioButton, QButtonGroup, QHBoxLayout, QListWidget, 
                             QListWidgetItem, QCheckBox, QGroupBox)
from PyQt5.QtGui import QFont, QColor
from PyQt5.QtCore import Qt, QThread, pyqtSignal

class Worker(QThread):
    # Các tín hiệu tương tác với luồng chính (UI)
    progress_updated = pyqtSignal(int)
    file_processed = pyqtSignal(int, str, bool) # Chỉ mục, thông điệp hiển thị, trạng thái thành công
    task_completed = pyqtSignal(str) # Đường dẫn tệp kết quả sau khi hoàn thành
    
    def __init__(self, file_paths, output_path):
        super().__init__()
        self.file_paths = file_paths
        self.output_path = output_path
    
    def run(self):
        if not self.file_paths:
            self.task_completed.emit("")
            return

        total_files = len(self.file_paths)
        output_file_path = os.path.join(self.output_path, "merged_web_project_data.txt")

        try:
            with open(output_file_path, 'w', encoding='utf-8') as outfile:
                for i, input_file_path in enumerate(self.file_paths):
                    filename = os.path.basename(input_file_path)
                    try:
                        # Đọc tệp tin với mã hóa UTF-8, tự động bỏ qua các ký tự lỗi định dạng
                        with open(input_file_path, 'r', encoding='utf-8', errors='ignore') as infile:
                            content = infile.read()
                        
                        # Ghi vào tệp tổng với cấu trúc phân định rõ ràng bằng thẻ tiêu đề
                        outfile.write(f"{'='*80}\n")
                        outfile.write(f"ĐƯỜNG DẪN TỆP: {input_file_path}\n")
                        outfile.write(f"{'='*80}\n\n")
                        outfile.write(f"{content}\n\n")
                        
                        # Thông báo lên UI: Thành công
                        self.file_processed.emit(i, f"✅ {filename}", True)
                    except Exception as e:
                        # Thông báo lên UI: Thất bại kèm lỗi chi tiết
                        self.file_processed.emit(i, f"❌ {filename} (Lỗi: {str(e)})", False)
                    
                    # Tính toán và cập nhật thanh tiến trình chạy
                    progress = int(((i + 1) / total_files) * 100)
                    self.progress_updated.emit(progress)
            
            self.task_completed.emit(output_file_path)
        except Exception as e:
            print(f"Lỗi hệ thống khi ghi tệp: {e}")
            self.task_completed.emit("")

class WebCodeAggregator(QWidget):
    def __init__(self):
        super().__init__()
        
        # Danh sách thư mục rác/môi trường phát triển cần bỏ qua trong dự án Web
        self.ignore_dirs = {
            '.git', 'node_modules', 'dist', 'build', '.next', '.nuxt', 'out', 
            '.cache', 'coverage', '.vscode', '.idea', 'bower_components'
        }
        
        # Danh sách các tệp tự động sinh hoặc tệp nén cần loại bỏ để tránh làm nặng file gộp
        self.ignore_files = {
            'package-lock.json', 'yarn.lock', 'pnpm-lock.yaml', 'bun.lockb'
        }
        
        self.all_files = []
        self.initUI()

    def initUI(self):
        self.setWindowTitle('Trình Gộp Mã Nguồn Dự Án Web (TS, HTML, CSS, JS)')
        self.setGeometry(200, 100, 950, 800)
        self.setStyleSheet("background-color: #f3f4f6;") # Tone xám sáng dịu mắt hiện đại
        
        layout = QVBoxLayout()
        layout.setSpacing(12)
        layout.setContentsMargins(15, 15, 15, 15)

        # 1. Khu vực chọn chế độ quét
        mode_group = QGroupBox("1. Chế độ quét nguồn")
        mode_group.setFont(QFont('Segoe UI', 10, QFont.Bold))
        mode_layout = QHBoxLayout()
        
        self.btn_group = QButtonGroup(self)
        self.radio_folder = QRadioButton("Quét toàn bộ thư mục dự án")
        self.radio_files = QRadioButton("Chọn thủ công từng tệp tin")
        self.radio_folder.setChecked(True)
        
        for rb in [self.radio_folder, self.radio_files]:
            rb.setFont(QFont('Segoe UI', 10))
            self.btn_group.addButton(rb)
            mode_layout.addWidget(rb)
        mode_layout.addStretch()
        mode_group.setLayout(mode_layout)
        layout.addWidget(mode_group)

        # 2. Khu vực lọc định dạng tệp (Động theo yêu cầu Web)
        filter_group = QGroupBox("2. Bộ lọc định dạng tệp tin muốn giữ lại")
        filter_group.setFont(QFont('Segoe UI', 10, QFont.Bold))
        filter_layout = QHBoxLayout()
        
        self.chk_ts = QCheckBox("TypeScript (.ts, .tsx)")
        self.chk_html = QCheckBox("HTML (.html, .htm)")
        self.chk_css = QCheckBox("CSS/SCSS/Sass (.css, .scss, .sass)")
        self.chk_js = QCheckBox("JavaScript (.js, .jsx)")
        self.chk_other = QCheckBox("Khác (.json, .md, .env)")
        
        # Mặc định tích chọn tất cả
        for chk in [self.chk_ts, self.chk_html, self.chk_css, self.chk_js, self.chk_other]:
            chk.setFont(QFont('Segoe UI', 10))
            chk.setChecked(True)
            chk.stateChanged.connect(self.on_filter_changed)
            filter_layout.addWidget(chk)
            
        filter_group.setLayout(filter_layout)
        layout.addWidget(filter_group)

        # 3. Nút chọn nguồn và Nơi lưu tệp đầu ra
        btn_layout = QHBoxLayout()
        self.btn_in = QPushButton("📁 Chọn Nguồn Phát Hiện")
        self.btn_out = QPushButton("💾 Chọn Thư Mục Lưu")
        
        for btn in [self.btn_in, self.btn_out]:
            btn.setFont(QFont('Segoe UI', 10, QFont.Bold))
            btn.setMinimumHeight(45)
            btn.setStyleSheet("""
                QPushButton { 
                    background-color: #4f46e5; 
                    color: white; 
                    border-radius: 6px; 
                    border: none;
                }
                QPushButton:hover { 
                    background-color: #4338ca; 
                }
            """)
            btn_layout.addWidget(btn)
        layout.addLayout(btn_layout)

        self.lbl_out = QLabel("Chưa chọn nơi lưu tệp kết quả...")
        self.lbl_out.setStyleSheet("color: #6b7280; font-style: italic; font-size: 11px;")
        layout.addWidget(self.lbl_out)

        # 4. Danh sách tệp tin hiển thị trực quan
        list_header_layout = QHBoxLayout()
        lbl_list_title = QLabel("<b>Danh sách các tệp tin phát hiện:</b>")
        lbl_list_title.setFont(QFont('Segoe UI', 10))
        list_header_layout.addWidget(lbl_list_title)
        
        # Nhãn hiển thị số lượng file hoạt động
        self.lbl_file_count = QLabel("0 tệp")
        self.lbl_file_count.setStyleSheet("color: #d97706; font-weight: bold;")
        self.lbl_file_count.setAlignment(Qt.AlignRight | Qt.AlignVCenter)
        list_header_layout.addWidget(self.lbl_file_count)
        layout.addLayout(list_header_layout)

        self.file_list = QListWidget()
        self.file_list.setStyleSheet("""
            QListWidget { 
                background-color: white; 
                border: 1px solid #d1d5db; 
                border-radius: 6px; 
                font-family: 'Consolas', 'Courier New', monospace; 
                font-size: 12px;
            }
            QListWidget::item { 
                padding: 6px; 
                border-bottom: 1px solid #f3f4f6; 
            }
        """)
        layout.addWidget(self.file_list)

        # 5. Thanh tiến độ xử lý dữ liệu
        self.pbar = QProgressBar()
        self.pbar.setValue(0)
        self.pbar.setStyleSheet("""
            QProgressBar { 
                height: 24px; 
                border: 1px solid #d1d5db; 
                border-radius: 6px; 
                text-align: center; 
                font-weight: bold;
                background-color: #e5e7eb;
            }
            QProgressBar::chunk { 
                background-color: #10b981; 
                border-radius: 5px;
            }
        """)
        layout.addWidget(self.pbar)

        # 6. Nút bắt đầu hành động chính
        self.btn_run = QPushButton("🚀 BẮT ĐẦU CHUYỂN ĐỔI & GỘP MÃ NGUỒN")
        self.btn_run.setEnabled(False)
        self.btn_run.setMinimumHeight(55)
        self.btn_run.setStyleSheet("""
            QPushButton { 
                background-color: #10b981; 
                color: white; 
                border-radius: 6px; 
                font-size: 15px; 
                font-weight: bold; 
                border: none;
            }
            QPushButton:hover { 
                background-color: #059669; 
            }
            QPushButton:disabled { 
                background-color: #9ca3af; 
                color: #e5e7eb; 
            }
        """)
        layout.addWidget(self.btn_run)

        # Kết nối các sự kiện kích hoạt
        self.btn_in.clicked.connect(self.browse_in)
        self.btn_out.clicked.connect(self.browse_out)
        self.btn_run.clicked.connect(self.start_worker)

        self.setLayout(layout)
        
        # Đường dẫn thư mục nguồn hiện tại lưu tạm
        self.current_source_path = ""

    def get_supported_extensions(self):
        """Xây dựng tuple các đuôi tệp tin được hỗ trợ dựa trên lựa chọn UI của người dùng"""
        exts = []
        if self.chk_ts.isChecked():
            exts.extend(['.ts', '.tsx'])
        if self.chk_html.isChecked():
            exts.extend(['.html', '.htm'])
        if self.chk_css.isChecked():
            exts.extend(['.css', '.scss', '.sass', '.less'])
        if self.chk_js.isChecked():
            exts.extend(['.js', '.jsx'])
        if self.chk_other.isChecked():
            exts.extend(['.json', '.md', '.env', '.config'])
        return tuple(exts)

    def on_filter_changed(self):
        """Tự động cập nhật lại danh sách tệp khi người dùng thay đổi bộ lọc checkbox"""
        if self.current_source_path or self.all_files:
            self.refresh_file_list()

    def refresh_file_list(self):
        """Quét và làm mới danh sách tệp tin hiển thị lên UI theo các tiêu chuẩn mới"""
        self.all_files = []
        self.file_list.clear()
        
        supported_exts = self.get_supported_extensions()
        if not supported_exts:
            self.lbl_file_count.setText("Đã tìm thấy: 0 tệp (Chưa chọn loại tệp nào)")
            self.update_run_button()
            return

        if self.radio_folder.isChecked() and self.current_source_path:
            # Quét đệ quy thư mục
            for root, dirs, files in os.walk(self.current_source_path):
                # Loại bỏ các thư mục rác của Web và Git
                dirs[:] = [d for d in dirs if d not in self.ignore_dirs]
                
                for f in files:
                    f_lower = f.lower()
                    # Không quét các file lock, file nén minified hoặc file sourcemap
                    if f_lower in self.ignore_files or f_lower.endswith(('.min.js', '.min.css', '.map')):
                        continue
                        
                    if f_lower.endswith(supported_exts):
                        self.all_files.append(os.path.join(root, f))
                        
        elif not self.radio_folder.isChecked() and hasattr(self, 'selected_raw_files'):
            # Lấy từ danh sách tệp được người dùng bôi đen thủ công
            for f in self.selected_raw_files:
                f_lower = os.path.basename(f).lower()
                if f_lower in self.ignore_files or f_lower.endswith(('.min.js', '.min.css', '.map')):
                    continue
                if f_lower.endswith(supported_exts):
                    self.all_files.append(f)

        # Cập nhật thông tin hiển thị danh sách lên widget
        for f in self.all_files:
            item = QListWidgetItem(f"⏳ {os.path.basename(f)}")
            self.file_list.addItem(item)
        
        self.lbl_file_count.setText(f"Đã tìm thấy: {len(self.all_files)} tệp tin thỏa mãn")
        self.update_run_button()

    def browse_in(self):
        """Xử lý sự kiện nhấn nút Chọn Nguồn đầu vào"""
        if self.radio_folder.isChecked():
            root_dir = QFileDialog.getExistingDirectory(self, "Chọn thư mục dự án Web")
            if root_dir:
                self.current_source_path = root_dir
                self.selected_raw_files = []
                self.refresh_file_list()
        else:
            files, _ = QFileDialog.getOpenFileNames(
                self, "Chọn các tệp mã nguồn", "", 
                "Web Code Files (*.ts *.tsx *.js *.jsx *.html *.css *.scss *.json *.md *.env)"
            )
            if files:
                self.current_source_path = ""
                self.selected_raw_files = files
                self.refresh_file_list()

    def browse_out(self):
        """Chọn nơi lưu trữ tệp kết quả đầu ra"""
        path = QFileDialog.getExistingDirectory(self, "Chọn thư mục để lưu kết quả")
        if path:
            self.lbl_out.setText(path)
        self.update_run_button()

    def update_run_button(self):
        """Quản lý trạng thái ẩn/hiện kích hoạt của nút Gộp mã nguồn"""
        can_run = len(self.all_files) > 0 and self.lbl_out.text() != "Chưa chọn nơi lưu tệp kết quả..."
        self.btn_run.setEnabled(can_run)

    def start_worker(self):
        """Khởi chạy luồng ngầm thực thi việc ghép tệp văn bản"""
        self.btn_run.setEnabled(False)
        self.btn_in.setEnabled(False)
        self.pbar.setValue(0)
        
        # Khởi tạo tiến trình Worker hoạt động ngầm (Multi-threading)
        self.worker = Worker(self.all_files, self.lbl_out.text())
        self.worker.progress_updated.connect(self.pbar.setValue)
        self.worker.file_processed.connect(self.update_item_status)
        self.worker.task_completed.connect(self.on_finished)
        self.worker.start()

    def update_item_status(self, index, message, success):
        """Cập nhật trạng thái đổi màu từng dòng tệp hiển thị theo thời gian thực"""
        item = self.file_list.item(index)
        if item:
            item.setText(message)
            if success:
                item.setForeground(QColor("#10b981")) # Xanh lá đại diện cho thành công
            else:
                item.setForeground(QColor("#ef4444")) # Màu đỏ báo lỗi đọc file
            self.file_list.scrollToItem(item)

    def on_finished(self, output_file_path):
        """Hoàn tất quá trình gộp mã nguồn dự án"""
        self.btn_in.setEnabled(True)
        self.update_run_button()
        
        if output_file_path:
            QMessageBox.information(
                self, 
                "Thành công", 
                f"Đã hoàn thành gộp toàn bộ mã nguồn web!\nTệp tổng hợp được lưu tại:\n{output_file_path}"
            )
        else:
            QMessageBox.warning(
                self, 
                "Thông báo lỗi", 
                "Quá trình xử lý thất bại hoặc không có dữ liệu tệp hợp lệ."
            )

if __name__ == '__main__':
    app = QApplication(sys.argv)
    # Áp dụng phong cách Fusion hiện đại cho toàn hệ thống
    app.setStyle('Fusion')
    ex = WebCodeAggregator()
    ex.show()
    sys.exit(app.exec_())