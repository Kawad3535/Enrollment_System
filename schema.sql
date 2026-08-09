-- Online Enrollment System — schema and seed data
-- Run with: mysql -u root -p < schema.sql

CREATE DATABASE IF NOT EXISTS enrollment_system;
USE enrollment_system;

CREATE TABLE users (
  id INT AUTO_INCREMENT PRIMARY KEY,
  name VARCHAR(150) NOT NULL,
  email VARCHAR(150) NOT NULL UNIQUE,
  password_hash VARCHAR(255) NOT NULL,
  role ENUM('student','dean','registrar','cashier') NOT NULL DEFAULT 'student',
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE student_profiles (
  id INT AUTO_INCREMENT PRIMARY KEY,
  user_id INT NOT NULL UNIQUE,
  birthdate DATE NULL,
  address VARCHAR(255) NULL,
  contact_number VARCHAR(50) NULL,
  previous_school VARCHAR(255) NULL,
  previous_program VARCHAR(150) NULL,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE TABLE programs (
  id INT AUTO_INCREMENT PRIMARY KEY,
  code VARCHAR(20) NOT NULL UNIQUE,
  name VARCHAR(100) NOT NULL,
  unit_rate DECIMAL(10,2) NOT NULL DEFAULT 500.00,
  misc_fee DECIMAL(10,2) NOT NULL DEFAULT 1500.00
);

CREATE TABLE subjects (
  id INT AUTO_INCREMENT PRIMARY KEY,
  program_id INT NOT NULL,
  code VARCHAR(20) NOT NULL,
  title VARCHAR(150) NOT NULL,
  units INT NOT NULL DEFAULT 3,
  FOREIGN KEY (program_id) REFERENCES programs(id) ON DELETE CASCADE
);

CREATE TABLE enrollment_applications (
  id INT AUTO_INCREMENT PRIMARY KEY,
  student_id INT NOT NULL,
  program_id INT NOT NULL,
  status ENUM(
    'dean_review','dean_returned',
    'registrar_review','registrar_returned',
    'for_payment','enrolled'
  ) NOT NULL DEFAULT 'dean_review',
  dean_remarks VARCHAR(500) NULL,
  return_reason VARCHAR(500) NULL,
  dean_id INT NULL,
  registrar_id INT NULL,
  cashier_id INT NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  FOREIGN KEY (student_id) REFERENCES users(id),
  FOREIGN KEY (program_id) REFERENCES programs(id)
);

CREATE TABLE application_subjects (
  id INT AUTO_INCREMENT PRIMARY KEY,
  application_id INT NOT NULL,
  subject_id INT NOT NULL,
  FOREIGN KEY (application_id) REFERENCES enrollment_applications(id) ON DELETE CASCADE,
  FOREIGN KEY (subject_id) REFERENCES subjects(id),
  UNIQUE KEY uniq_app_subject (application_id, subject_id)
);

CREATE TABLE documents (
  id INT AUTO_INCREMENT PRIMARY KEY,
  application_id INT NOT NULL,
  original_name VARCHAR(255) NOT NULL,
  stored_path VARCHAR(255) NOT NULL,
  uploaded_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY (application_id) REFERENCES enrollment_applications(id) ON DELETE CASCADE
);

CREATE TABLE payments (
  id INT AUTO_INCREMENT PRIMARY KEY,
  application_id INT NOT NULL UNIQUE,
  total_units INT NOT NULL,
  tuition_fee DECIMAL(10,2) NOT NULL,
  misc_fee DECIMAL(10,2) NOT NULL,
  total_amount DECIMAL(10,2) NOT NULL,
  status ENUM('pending','confirmed') NOT NULL DEFAULT 'pending',
  confirmed_at DATETIME NULL,
  FOREIGN KEY (application_id) REFERENCES enrollment_applications(id) ON DELETE CASCADE
);

CREATE TABLE application_history (
  id INT AUTO_INCREMENT PRIMARY KEY,
  application_id INT NOT NULL,
  stage VARCHAR(50) NOT NULL,
  action VARCHAR(255) NOT NULL,
  actor_name VARCHAR(150) NOT NULL,
  actor_role VARCHAR(50) NOT NULL,
  note VARCHAR(500) NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY (application_id) REFERENCES enrollment_applications(id) ON DELETE CASCADE
);

-- Programs: Degree / Masteral / TCP only, per spec
INSERT INTO programs (code,name,unit_rate,misc_fee) VALUES
 ('DEG','Degree Program',500.00,1500.00),
 ('MAST','Masteral Program',800.00,2000.00),
 ('TCP','Teacher Certificate Program (TCP)',600.00,1800.00);

-- Sample subject bank per program (Dean picks from these when evaluating)
INSERT INTO subjects (program_id, code, title, units) VALUES
 (1,'GE101','Purposive Communication',3),
 (1,'GE102','Mathematics in the Modern World',3),
 (1,'IT201','Data Structures and Algorithms',3),
 (1,'IT202','Database Management Systems',3),
 (1,'IT203','Systems Integration and Architecture',3),
 (2,'ED701','Advanced Educational Research',3),
 (2,'ED702','Curriculum Development',3),
 (2,'ED703','Educational Leadership and Management',3),
 (2,'ED704','Advanced Statistics for Education',3),
 (3,'TCP101','Foundations of Education',3),
 (3,'TCP102','Child and Adolescent Development',3),
 (3,'TCP103','Principles of Teaching',3),
 (3,'TCP104','Assessment of Learning',3);

-- Staff accounts (dean / registrar / cashier) are not self-registered.
-- After the schema is loaded, run:  go run . --seed-staff
-- This creates dean@school.edu / registrar@school.edu / cashier@school.edu
-- all with password: password123
